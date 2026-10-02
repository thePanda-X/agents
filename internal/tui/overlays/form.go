package overlays

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/google/jsonschema-go/jsonschema"
)

type Field struct {
	Name, Label, Type, Description string
	Required                       bool
	Choices                        []string
	Options                        []FormOption
	ChoiceCursor                   int
	Input                          textinput.Model
}

type FormOption struct{ Value, Label, Description, Preview string }

type Form struct {
	Fields           []Field
	Cursor           int
	Review           bool
	Error            string
	Schema           *jsonschema.Resolved
	Content          map[string]any
	Message          string
	scroll, pageRows int
	reveal           bool
}

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
	slices.Sort(names)
	for _, name := range names {
		property := schema.Properties[name]
		if property == nil {
			return nil, fmt.Errorf("missing schema for %q", name)
		}
		field := Field{Name: name, Label: property.Title, Description: property.Description, Type: property.Type, Required: slices.Contains(schema.Required, name)}
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
			for _, option := range field.Options {
				field.Choices = append(field.Choices, option.Value)
			}
		}
		field.Input = textinput.New()
		field.Input.Prompt = "  "
		field.Input.CharLimit = 8192
		field.Input.SetWidth(45)
		if property.Default != nil {
			var text string
			if err := json.Unmarshal(property.Default, &text); err == nil {
				field.Input.SetValue(text)
			} else {
				field.Input.SetValue(string(property.Default))
			}
		} else if field.Type == "boolean" {
			field.Input.SetValue("false")
		}
		if index := slices.Index(field.Choices, field.Input.Value()); index >= 0 {
			field.ChoiceCursor = index
		}
		form.Fields = append(form.Fields, field)
	}
	if len(form.Fields) > 0 {
		form.focus(0)
	}
	return form, nil
}

func (f *Form) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if key.String() == "pgup" || key.String() == "pgdown" {
			step := max(1, f.pageRows)
			if key.String() == "pgup" {
				step = -step
			}
			f.scroll = max(0, f.scroll+step)
			f.reveal = false
			return nil
		}
		if len(f.Fields) == 0 || f.Review {
			return nil
		}
		field := &f.Fields[f.Cursor]
		switch key.String() {
		case "tab":
			return f.focus((f.Cursor + 1) % len(f.Fields))
		case "shift+tab":
			return f.focus((f.Cursor + len(f.Fields) - 1) % len(f.Fields))
		case "up", "down":
			if len(field.Choices) > 0 {
				step := 1
				if key.String() == "up" {
					step = -1
				}
				field.ChoiceCursor = (field.ChoiceCursor + step + len(field.Choices)) % len(field.Choices)
				f.reveal = true
				return nil
			}
			if key.String() == "up" {
				return f.focus((f.Cursor + len(f.Fields) - 1) % len(f.Fields))
			}
			if key.String() == "down" {
				return f.focus((f.Cursor + 1) % len(f.Fields))
			}
		case "space", "enter", "left", "right":
			if field.Type == "boolean" {
				field.Input.SetValue(strconv.FormatBool(field.Input.Value() != "true"))
				f.reveal = true
				return nil
			}
			if len(field.Choices) > 0 {
				if key.String() == "left" || key.String() == "right" {
					step := 1
					if key.String() == "left" {
						step = -1
					}
					field.ChoiceCursor = (field.ChoiceCursor + step + len(field.Choices)) % len(field.Choices)
					f.reveal = true
					return nil
				}
				field.toggleChoice()
				f.reveal = true
				return nil
			}
			if key.String() == "enter" {
				return f.focus((f.Cursor + 1) % len(f.Fields))
			}
		case "backspace", "delete":
			if len(field.Choices) > 0 {
				field.Input.SetValue("")
				return nil
			}
		}
	}
	if len(f.Fields) == 0 || f.Review {
		return nil
	}
	if len(f.Fields[f.Cursor].Choices) > 0 || f.Fields[f.Cursor].Type == "boolean" {
		return nil
	}
	var cmd tea.Cmd
	f.Fields[f.Cursor].Input, cmd = f.Fields[f.Cursor].Input.Update(msg)
	return cmd
}

func (f *Form) focus(index int) tea.Cmd {
	f.Fields[f.Cursor].Input.Blur()
	f.Cursor = index
	f.scroll, f.reveal = 0, true
	if len(f.Fields[index].Choices) > 0 || f.Fields[index].Type == "boolean" {
		return nil
	}
	return f.Fields[index].Input.Focus()
}

func (f *Form) Edit() tea.Cmd {
	f.Review, f.scroll, f.reveal = false, 0, true
	if len(f.Fields) == 0 {
		return nil
	}
	return f.focus(f.Cursor)
}

func (field *Field) selectedValues() []string {
	var values []string
	if field.Type == "array" {
		_ = json.Unmarshal([]byte(field.Input.Value()), &values)
	} else if field.Input.Value() != "" {
		values = []string{field.Input.Value()}
	}
	return values
}

func (field *Field) toggleChoice() {
	value := field.Choices[field.ChoiceCursor]
	values := field.selectedValues()
	if field.Type != "array" {
		if field.Input.Value() == value && !field.Required {
			field.Input.SetValue("")
		} else {
			field.Input.SetValue(value)
		}
		return
	}
	if slices.Contains(values, value) {
		values = slices.DeleteFunc(values, func(s string) bool { return s == value })
	} else {
		values = append(values, value)
	}
	// Serialize in schema order, not selection order, and never comma-join values.
	ordered := []string{}
	for _, choice := range field.Choices {
		if slices.Contains(values, choice) {
			ordered = append(ordered, choice)
		}
	}
	if len(ordered) == 0 && !field.Required {
		field.Input.SetValue("")
		return
	}
	data, _ := json.Marshal(ordered)
	field.Input.SetValue(string(data))
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

func (f *Form) Prepare() bool {
	content := make(map[string]any)
	for index, field := range f.Fields {
		text := field.Input.Value()
		if text == "" && !field.Required {
			continue
		}
		var result any
		var err error
		switch field.Type {
		case "string":
			result = text
		case "integer":
			result, err = strconv.ParseInt(text, 10, 64)
		case "number":
			result, err = strconv.ParseFloat(text, 64)
		case "boolean":
			result, err = strconv.ParseBool(text)
		case "array":
			var values []string
			err = json.Unmarshal([]byte(text), &values)
			result = values
		}
		if err != nil {
			f.Error = field.Label + ": " + err.Error()
			f.focus(index)
			return false
		}
		content[field.Name] = result
	}
	if err := f.Schema.Validate(content); err != nil {
		f.Error = err.Error()
		return false
	}
	f.Content, f.Review, f.Error = content, true, ""
	f.scroll, f.reveal = 0, false
	return true
}
