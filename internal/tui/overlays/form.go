package overlays

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/google/jsonschema-go/jsonschema"
)

// Action is a form-level outcome that the TUI must turn into an interaction
// decision. Text editing and navigation return ActionNone.
type Action int

const (
	ActionNone Action = iota
	ActionSubmit
	ActionSkip
)

type Field struct {
	Name, Label, Description, Type string
	Required                       bool
	Options                        []FormOption
	Selected                       []string
	Cursor                         int
	Input                          textinput.Model
}

type Question struct {
	Field *Field
	// Other is the optional companion free-text field supplied by the agent.
	// Claude's AskUserQuestion bridge pairs question_<n> with
	// question_<n>_custom, so it belongs to the question rather than the form.
	Other *Field
}

type Form struct {
	Questions    []*Question
	Cursor       int
	Review       bool
	ReviewCursor int
	Error        string
	Schema       *jsonschema.Resolved
	Content      map[string]any
	Message      string
	scroll       int
	pageRows     int
	reveal       bool
	// returnToReview sends Enter back to the review page after editing one
	// answer, instead of advancing through the remaining questions.
	returnToReview bool
}

type FormOption struct{ Value, Label, Description, Preview string }

func NewForm(raw json.RawMessage) (*Form, error) {
	if len(raw) > 256<<10 {
		return nil, errors.New("question schema exceeds 256 KiB")
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	if schema.Type != "object" || len(schema.Properties) > 64 {
		return nil, errors.New("form requires a flat object with at most 64 fields")
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, err
	}
	form := &Form{Schema: resolved, reveal: true}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	slices.SortFunc(names, naturalCompare)

	// A companion is only an "Other" answer when it follows a select field
	// with the same base name. Otherwise it is an ordinary question.
	companions := make(map[string]string)
	for _, name := range names {
		base, ok := companionBase(name)
		if !ok {
			continue
		}
		property, companion := schema.Properties[base], schema.Properties[name]
		if property == nil || companion == nil || !isSelectSchema(property) || !isPlainString(companion) {
			continue
		}
		companions[name] = base
	}

	for _, name := range names {
		if _, companion := companions[name]; companion {
			continue
		}
		property := schema.Properties[name]
		if property == nil {
			return nil, fmt.Errorf("missing schema for %q", name)
		}
		field, err := newField(name, property, slices.Contains(schema.Required, name))
		if err != nil {
			return nil, err
		}
		form.Questions = append(form.Questions, &Question{Field: field})
	}
	for _, name := range names {
		base, ok := companions[name]
		if !ok {
			continue
		}
		var question *Question
		for _, candidate := range form.Questions {
			if candidate.Field.Name == base {
				question = candidate
				break
			}
		}
		if question == nil {
			return nil, fmt.Errorf("question companion %q has no question", name)
		}
		other, err := newField(name, schema.Properties[name], slices.Contains(schema.Required, name))
		if err != nil {
			return nil, err
		}
		question.Other = other
	}
	if len(form.Questions) > 0 {
		form.focusQuestion(0)
	}
	return form, nil
}

// companionBase reports the question key for a Claude-style custom-answer
// field. The installed bridge uses "_custom"; "_other" covers the older shape.
func companionBase(name string) (string, bool) {
	for _, suffix := range []string{"_custom", "_other"} {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return strings.TrimSuffix(name, suffix), true
		}
	}
	return "", false
}

func isSelectSchema(schema *jsonschema.Schema) bool {
	switch schema.Type {
	case "string":
		return len(schema.Enum) > 0 || len(schema.OneOf) > 0 || len(schema.AnyOf) > 0
	case "array":
		items := schema.Items
		return items != nil && (len(items.Enum) > 0 || len(items.OneOf) > 0 || len(items.AnyOf) > 0)
	}
	return false
}

func isPlainString(schema *jsonschema.Schema) bool {
	return schema.Type == "string" && len(schema.Enum) == 0 && len(schema.OneOf) == 0 && len(schema.AnyOf) == 0
}

func newField(name string, property *jsonschema.Schema, required bool) (*Field, error) {
	field := &Field{Name: name, Label: property.Title, Description: property.Description, Type: property.Type, Required: required}
	if field.Label == "" {
		field.Label = name
	}
	if field.Type == "" {
		field.Type = "string"
	}
	switch field.Type {
	case "string", "integer", "number", "boolean", "array":
	default:
		return nil, fmt.Errorf("unsupported field type %q", field.Type)
	}
	choiceSchema := property
	if field.Type == "array" {
		choiceSchema = property.Items
		if choiceSchema != nil && (choiceSchema.Type == "object" || choiceSchema.Type == "array") {
			return nil, errors.New("question arrays must contain primitive values")
		}
	}
	if field.Type == "string" || field.Type == "array" {
		field.Options = formOptions(choiceSchema)
		if len(field.Options) > 256 {
			return nil, errors.New("question field exceeds 256 options")
		}
	}
	if field.Type == "boolean" {
		field.Options = []FormOption{{Value: "false", Label: "No"}, {Value: "true", Label: "Yes"}}
	}
	field.Input = textinput.New()
	field.Input.Prompt = "  "
	field.Input.CharLimit = 8192
	field.Input.SetWidth(45)
	field.applyDefault(property.Default)
	if field.Type == "boolean" && len(field.Selected) == 0 {
		field.Selected = []string{"false"}
	}
	if len(field.Selected) > 0 {
		for index, option := range field.Options {
			if slices.Contains(field.Selected, option.Value) {
				field.Cursor = index
				break
			}
		}
	}
	return field, nil
}

func (field *Field) applyDefault(raw json.RawMessage) {
	if raw == nil {
		return
	}
	switch field.Type {
	case "string", "array":
		if len(field.Options) > 0 {
			if field.Type == "array" {
				var values []string
				if err := json.Unmarshal(raw, &values); err == nil {
					field.Selected = orderedValues(field.Options, values)
					return
				}
			}
			var text string
			if err := json.Unmarshal(raw, &text); err == nil && slices.Contains(fieldOptionValues(field.Options), text) {
				field.Selected = []string{text}
			}
			return
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			field.Input.SetValue(text)
		} else {
			field.Input.SetValue(string(raw))
		}
	case "integer", "number":
		field.Input.SetValue(string(raw))
	case "boolean":
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			field.Selected = []string{strconv.FormatBool(value)}
		}
	}
}

func fieldOptionValues(options []FormOption) []string {
	values := make([]string, len(options))
	for index, option := range options {
		values[index] = option.Value
	}
	return values
}

func orderedValues(options []FormOption, values []string) []string {
	var ordered []string
	for _, option := range options {
		if slices.Contains(values, option.Value) {
			ordered = append(ordered, option.Value)
		}
	}
	return ordered
}

func formOptions(schema *jsonschema.Schema) []FormOption {
	if schema == nil {
		return nil
	}
	var options []FormOption
	labels, _ := schema.Extra["enumNames"].([]any)
	for index, value := range schema.Enum {
		if text, ok := value.(string); ok {
			label := text
			if index < len(labels) {
				if title, ok := labels[index].(string); ok {
					label = title
				}
			}
			options = append(options, FormOption{Value: text, Label: label})
		}
	}
	for _, choices := range [][]*jsonschema.Schema{schema.OneOf, schema.AnyOf} {
		for _, choice := range choices {
			if choice == nil || choice.Const == nil {
				continue
			}
			if value, ok := (*choice.Const).(string); ok {
				label := choice.Title
				if label == "" {
					label = value
				}
				option := FormOption{Value: value, Label: label, Description: choice.Description}
				if meta, ok := choice.Extra["_meta"].(map[string]any); ok {
					if info, ok := meta["_claude/askUserQuestionOption"].(map[string]any); ok {
						option.Preview, _ = info["preview"].(string)
					}
				}
				options = append(options, option)
			}
		}
	}
	return options
}

func (f *Form) Key(msg tea.KeyPressMsg) (Action, tea.Cmd) {
	key := msg.String()
	if key == "pgup" || key == "pgdown" {
		step := max(1, f.pageRows)
		if key == "pgup" {
			step = -step
		}
		f.scroll = max(0, f.scroll+step)
		f.reveal = false
		return ActionNone, nil
	}
	if key == "ctrl+enter" {
		if f.Submit() {
			return ActionSubmit, nil
		}
		return ActionNone, nil
	}
	if f.Review {
		return f.reviewKey(key)
	}
	if len(f.Questions) == 0 {
		if key == "enter" && f.Submit() {
			return ActionSubmit, nil
		}
		return ActionNone, nil
	}
	question := f.Questions[f.Cursor]
	if key == "tab" {
		return ActionNone, f.focusQuestion((f.Cursor + 1) % len(f.Questions))
	}
	if key == "shift+tab" {
		return ActionNone, f.focusQuestion((f.Cursor + len(f.Questions) - 1) % len(f.Questions))
	}
	if key == "esc" {
		if question.onOther() {
			question.Field.Cursor = 0
			question.Other.Input.Blur()
			f.reveal = true
			return ActionNone, nil
		}
		return ActionSkip, nil
	}
	if question.onOther() {
		switch key {
		case "up":
			return ActionNone, f.moveCursor(-1)
		case "down":
			return ActionNone, f.moveCursor(1)
		case "enter":
			return f.advance()
		default:
			before := question.Other.Input.Value()
			var cmd tea.Cmd
			question.Other.Input, cmd = question.Other.Input.Update(msg)
			if before != question.Other.Input.Value() {
				f.Error = ""
				// Claude's single-select "Other" replaces the choice; the
				// bridge records custom text alone as the answer.
				if question.Field.Type != "array" {
					question.Field.Selected = nil
				}
				f.reveal = true
			}
			return ActionNone, cmd
		}
	}
	if len(question.Field.Options) == 0 {
		switch key {
		case "up":
			return ActionNone, f.focusQuestion((f.Cursor + len(f.Questions) - 1) % len(f.Questions))
		case "down":
			return ActionNone, f.focusQuestion((f.Cursor + 1) % len(f.Questions))
		case "enter":
			return f.advance()
		default:
			before := question.Field.Input.Value()
			var cmd tea.Cmd
			question.Field.Input, cmd = question.Field.Input.Update(msg)
			if before != question.Field.Input.Value() {
				f.Error = ""
				f.reveal = true
			}
			return ActionNone, cmd
		}
	}
	switch key {
	case "up":
		return ActionNone, f.moveCursor(-1)
	case "down":
		return ActionNone, f.moveCursor(1)
	case "left":
		return ActionNone, f.focusQuestion((f.Cursor + len(f.Questions) - 1) % len(f.Questions))
	case "right":
		return ActionNone, f.focusQuestion((f.Cursor + 1) % len(f.Questions))
	case "space":
		question.chooseCurrent()
		f.Error = ""
		f.reveal = true
		return ActionNone, nil
	case "enter":
		if question.Field.Type != "array" {
			question.chooseCurrent()
		}
		return f.advance()
	case "delete", "backspace":
		question.Field.Selected = nil
		f.Error = ""
		f.reveal = true
		return ActionNone, nil
	}
	return ActionNone, nil
}

// Update routes paste and text-input blink messages to the focused input.
func (f *Form) Update(msg tea.Msg) tea.Cmd {
	if f.Review || len(f.Questions) == 0 {
		return nil
	}
	question := f.Questions[f.Cursor]
	if question.onOther() {
		var cmd tea.Cmd
		question.Other.Input, cmd = question.Other.Input.Update(msg)
		return cmd
	}
	if len(question.Field.Options) == 0 {
		var cmd tea.Cmd
		question.Field.Input, cmd = question.Field.Input.Update(msg)
		return cmd
	}
	return nil
}

func (f *Form) reviewKey(key string) (Action, tea.Cmd) {
	switch key {
	case "up":
		f.ReviewCursor = max(0, f.ReviewCursor-1)
		f.reveal = true
		return ActionNone, nil
	case "down":
		f.ReviewCursor = min(len(f.Questions), f.ReviewCursor+1)
		f.reveal = true
		return ActionNone, nil
	case "enter":
		if f.ReviewCursor >= len(f.Questions) {
			if f.Submit() {
				return ActionSubmit, nil
			}
			return ActionNone, nil
		}
		question := f.Questions[f.ReviewCursor]
		if question.Other != nil && strings.TrimSpace(question.Other.Input.Value()) != "" {
			question.Field.Cursor = question.otherIndex()
		} else if len(question.Field.Selected) > 0 {
			for index, option := range question.Field.Options {
				if slices.Contains(question.Field.Selected, option.Value) {
					question.Field.Cursor = index
					break
				}
			}
		}
		cmd := f.focusQuestion(f.ReviewCursor)
		f.returnToReview = true
		return ActionNone, cmd
	case "esc":
		return ActionSkip, nil
	}
	return ActionNone, nil
}

func (f *Form) advance() (Action, tea.Cmd) {
	if f.returnToReview {
		index := f.Cursor
		f.enterReview()
		f.ReviewCursor = max(0, min(index, len(f.Questions)))
		return ActionNone, nil
	}
	if f.Cursor+1 < len(f.Questions) {
		return ActionNone, f.focusQuestion(f.Cursor + 1)
	}
	if len(f.Questions) == 1 {
		if f.Submit() {
			return ActionSubmit, nil
		}
		return ActionNone, nil
	}
	f.enterReview()
	return ActionNone, nil
}

func (f *Form) enterReview() {
	f.blurAll()
	f.Review = true
	f.returnToReview = false
	f.ReviewCursor = len(f.Questions)
	f.scroll, f.reveal = 0, true
	f.Error = ""
}

func (f *Form) focusQuestion(index int) tea.Cmd {
	f.blurAll()
	f.Cursor = index
	f.Review = false
	f.returnToReview = false
	f.scroll, f.reveal = 0, true
	f.Error = ""
	if index < 0 || index >= len(f.Questions) {
		return nil
	}
	question := f.Questions[index]
	question.Field.Cursor = max(0, min(question.Field.Cursor, question.lastCursor()))
	if question.onOther() {
		return question.Other.Input.Focus()
	}
	if len(question.Field.Options) == 0 {
		return question.Field.Input.Focus()
	}
	return nil
}

func (f *Form) blurAll() {
	for _, question := range f.Questions {
		question.Field.Input.Blur()
		if question.Other != nil {
			question.Other.Input.Blur()
		}
	}
}

func (f *Form) moveCursor(step int) tea.Cmd {
	if len(f.Questions) == 0 {
		return nil
	}
	question := f.Questions[f.Cursor]
	last := question.lastCursor()
	question.Field.Cursor = (question.Field.Cursor + step + last + 1) % (last + 1)
	f.reveal = true
	f.Error = ""
	if question.onOther() {
		return question.Other.Input.Focus()
	}
	if question.Other != nil {
		question.Other.Input.Blur()
	}
	return nil
}

func (f *Form) Submit() bool {
	for index, question := range f.Questions {
		if question.Field.Required && !question.answered() {
			f.focusQuestion(index)
			f.Error = question.Field.Label + " is required"
			return false
		}
		if question.Other != nil && question.Other.Required && strings.TrimSpace(question.Other.Input.Value()) == "" {
			question.Field.Cursor = question.otherIndex()
			f.focusQuestion(index)
			f.Error = question.Other.Label + " is required"
			return false
		}
	}
	content, err := f.snapshot()
	if err != nil {
		return false
	}
	if err := f.Schema.Validate(content); err != nil {
		f.Error = err.Error()
		return false
	}
	f.Content, f.Error = content, ""
	return true
}

func (f *Form) snapshot() (map[string]any, error) {
	content := make(map[string]any)
	for index, question := range f.Questions {
		field := question.Field
		if len(field.Options) > 0 {
			values := question.selectedValues()
			if len(values) > 0 {
				switch field.Type {
				case "array":
					content[field.Name] = append([]string(nil), values...)
				case "boolean":
					value, _ := strconv.ParseBool(values[0])
					content[field.Name] = value
				default:
					content[field.Name] = values[0]
				}
			}
		} else {
			text := field.Input.Value()
			if text == "" && !field.Required {
				continue
			}
			var value any
			var err error
			switch field.Type {
			case "string":
				value = text
			case "integer":
				value, err = strconv.ParseInt(text, 10, 64)
			case "number":
				value, err = strconv.ParseFloat(text, 64)
			case "array":
				var values []string
				err = json.Unmarshal([]byte(text), &values)
				value = values
			}
			if err != nil {
				f.focusQuestion(index)
				f.Error = field.Label + ": " + err.Error()
				return nil, err
			}
			content[field.Name] = value
		}
		if question.Other != nil {
			if text := question.Other.Input.Value(); strings.TrimSpace(text) != "" {
				content[question.Other.Name] = text
			}
		}
	}
	return content, nil
}

func (question *Question) lastCursor() int {
	if question.Other != nil {
		return len(question.Field.Options)
	}
	return max(0, len(question.Field.Options)-1)
}

func (question *Question) otherIndex() int {
	if question.Other == nil {
		return -1
	}
	return len(question.Field.Options)
}

func (question *Question) onOther() bool {
	return question.Other != nil && question.Field.Cursor == question.otherIndex()
}

func (question *Question) chooseCurrent() {
	if question.Field.Cursor < 0 || question.Field.Cursor >= len(question.Field.Options) {
		return
	}
	value := question.Field.Options[question.Field.Cursor].Value
	if question.Field.Type == "array" {
		if slices.Contains(question.Field.Selected, value) {
			question.Field.Selected = slices.DeleteFunc(question.Field.Selected, func(s string) bool { return s == value })
		} else {
			question.Field.Selected = append(question.Field.Selected, value)
		}
		question.Field.Selected = orderedValues(question.Field.Options, question.Field.Selected)
		return
	}
	question.Field.Selected = []string{value}
	if question.Other != nil {
		question.Other.Input.SetValue("")
	}
}

func (question *Question) selectedValues() []string {
	return orderedValues(question.Field.Options, question.Field.Selected)
}

func (question *Question) answered() bool {
	if question.Other != nil && strings.TrimSpace(question.Other.Input.Value()) != "" {
		return true
	}
	if len(question.Field.Options) > 0 {
		return len(question.Field.Selected) > 0
	}
	return strings.TrimSpace(question.Field.Input.Value()) != ""
}

func naturalCompare(a, b string) int {
	for i, j := 0, 0; i < len(a) && j < len(b); {
		if isDigit(a[i]) && isDigit(b[j]) {
			ia, jb := i, j
			for ia < len(a) && isDigit(a[ia]) {
				ia++
			}
			for jb < len(b) && isDigit(b[jb]) {
				jb++
			}
			na := strings.TrimLeft(a[i:ia], "0")
			nb := strings.TrimLeft(b[j:jb], "0")
			if na == "" {
				na = "0"
			}
			if nb == "" {
				nb = "0"
			}
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			i, j = ia, jb
			continue
		}
		if a[i] != b[j] {
			if a[i] < b[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func isDigit(value byte) bool {
	return value >= '0' && value <= '9'
}
