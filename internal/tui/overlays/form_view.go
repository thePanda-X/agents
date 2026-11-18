package overlays

import (
	"fmt"
	"slices"
	"strings"

	"agents/internal/tui/theme"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// View renders a question card, or the multi-question review. The title and
// question tabs stay fixed; the question body scrolls so long prompts, option
// descriptions, previews, and reviews remain readable in short terminals.
func (f *Form) View(width, height int) string {
	width, height = max(1, width), max(1, height)
	fixed := []string{f.title(width)}
	if !f.Review && len(f.Questions) > 1 {
		fixed = append(fixed, f.tabs(width))
	}
	rows, target := f.bodyRows(width)
	var errors []string
	if f.Error != "" {
		errors = styledLines(f.Error, width, theme.Error)
		if len(errors) > 2 {
			errors = errors[:2]
			errors[1] = ansi.Truncate(errors[1], max(1, width-1), "") + "…"
		}
	}
	scrollHint := false
	footer := f.footer(width, scrollHint)
	bodyHeight := f.bodyHeight(height, len(fixed), len(errors), len(footer))
	if len(rows) > bodyHeight {
		scrollHint = true
		footer = f.footer(width, scrollHint)
		bodyHeight = f.bodyHeight(height, len(fixed), len(errors), len(footer))
	}
	f.pageRows = max(1, bodyHeight)
	if f.reveal {
		if target < f.scroll {
			f.scroll = target
		}
		if target >= f.scroll+f.pageRows {
			f.scroll = target - f.pageRows + 1
		}
		f.reveal = false
	}
	f.scroll = min(max(0, f.scroll), max(0, len(rows)-f.pageRows))
	visible := append([]string(nil), rows[f.scroll:min(len(rows), f.scroll+f.pageRows)]...)
	for len(visible) < f.pageRows {
		visible = append(visible, "")
	}
	lines := append(append(append(append([]string(nil), fixed...), visible...), errors...), footer...)
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, width, "…")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (f *Form) bodyHeight(height, fixed, errors, footer int) int {
	remaining := height - fixed - errors - footer
	if remaining >= 1 {
		return remaining
	}
	// Keep at least one body row at very short heights. The final view is
	// clipped to the terminal budget if chrome still does not fit.
	return 1
}

func (f *Form) title(width int) string {
	title := "Question"
	if f.Title != "" {
		title = f.Title
	} else if f.Review {
		title = "Review answers"
	} else if len(f.Questions) > 1 {
		title = fmt.Sprintf("Questions · %d of %d", f.Cursor+1, len(f.Questions))
	} else if len(f.Questions) == 1 {
		title = "Question · " + f.Questions[0].Field.Label
	}
	return theme.Heading.Render(ansi.Truncate(oneLine(title), width, "…"))
}

func (f *Form) tabs(width int) string {
	var builder strings.Builder
	for index, question := range f.Questions {
		if index > 0 {
			builder.WriteString("   ")
		}
		marker, style := "○", theme.Quiet
		if index == f.Cursor {
			marker, style = "●", theme.Accent
		} else if question.answered() {
			marker, style = "✓", theme.Success
		}
		label := ansi.Truncate(oneLine(question.Field.Label), 16, "…")
		builder.WriteString(style.Render(marker + " " + label))
	}
	return ansi.Truncate(builder.String(), width, "…")
}

func (f *Form) bodyRows(width int) ([]string, int) {
	if f.Review {
		return f.reviewRows(width)
	}
	if len(f.Questions) == 0 {
		return []string{theme.Quiet.Render(ansi.Truncate("No questions were provided. Ctrl+Enter to continue.", width, "…"))}, 0
	}
	question := f.Questions[f.Cursor]
	rows := []string{}
	// The elicitation message is form-level context. The question's own
	// description is the prompt, so show each at most once even when the agent
	// repeats the message in both places.
	if f.Message != "" && f.Message != question.Field.Description {
		rows = appendWrapped(rows, f.Message, width, theme.Quiet)
		rows = append(rows, "")
	}
	if description := question.Field.Description; description != "" {
		rows = appendWrapped(rows, description, width, theme.Body)
		rows = append(rows, "")
	}
	target := 0
	if len(question.Field.Options) == 0 {
		target = len(rows)
		fitInput(&question.Field.Input, max(1, width-3))
		rows = append(rows, question.Field.Input.View())
		if question.Field.Type == "array" {
			rows = appendIndented(rows, "Enter a JSON array of strings.", width, 2, theme.Quiet)
		}
		return rows, target
	}
	selected := question.selectedValues()
	for index, option := range question.Field.Options {
		if index == question.Field.Cursor {
			target = len(rows)
		}
		prefix, style := "  ", theme.Body
		if index == question.Field.Cursor {
			prefix, style = "› ", theme.Accent
		}
		marker := optionMarker(question, option.Value, slices.Contains(selected, option.Value))
		row := prefix + marker + " " + oneLine(option.Label)
		rows = append(rows, style.Render(ansi.Truncate(row, width, "…")))
		if option.Description != "" {
			rows = appendIndented(rows, option.Description, width, 4, theme.Quiet)
		}
	}
	if question.Other != nil {
		if question.onOther() {
			target = len(rows)
		}
		rows = append(rows, f.otherRow(question, width))
		if question.onOther() {
			rows = appendIndented(rows, "Type an answer. Enter saves; Esc returns to the options.", width, 4, theme.Quiet)
		}
	}
	if option, ok := f.focusedOption(); ok && option.Preview != "" {
		rows = append(rows, "")
		rows = append(rows, theme.Heading.Render("Preview"))
		preview := styledLines(option.Preview, max(1, width-2), theme.Body)
		if len(preview) > 12 {
			preview = preview[:12]
			preview[11] = ansi.Truncate(preview[11], max(1, width-3), "") + "…"
		}
		rows = append(rows, preview...)
	}
	return rows, target
}

func (f *Form) otherRow(question *Question, width int) string {
	marker := "○"
	if question.Field.Type == "array" {
		marker = "[ ]"
	}
	text := oneLine(question.Other.Input.Value())
	if text != "" {
		if question.Field.Type == "array" {
			marker = "[x]"
		} else {
			marker = "●"
		}
	}
	if question.onOther() {
		question.Other.Input.Prompt = "Other: "
		question.Other.Input.Placeholder = "Type your own answer"
		// The prompt and row prefix render outside textinput's own width, and
		// the trailing cursor cell needs one more column.
		prefix := "› " + marker + " "
		fitInput(&question.Other.Input, max(1, width-lipgloss.Width(prefix)-lipgloss.Width(question.Other.Input.Prompt)-1))
		return theme.Accent.Render(prefix) + question.Other.Input.View()
	}
	label := "Other…"
	if text != "" {
		label = "Other: " + text
	}
	row := "  " + marker + " " + label
	style := theme.Body
	if text == "" {
		style = theme.Quiet
	}
	return style.Render(ansi.Truncate(row, width, "…"))
}

func (f *Form) focusedOption() (FormOption, bool) {
	if len(f.Questions) == 0 {
		return FormOption{}, false
	}
	question := f.Questions[f.Cursor]
	if question.Field.Cursor < 0 || question.Field.Cursor >= len(question.Field.Options) {
		return FormOption{}, false
	}
	return question.Field.Options[question.Field.Cursor], true
}

func (f *Form) reviewRows(width int) ([]string, int) {
	rows := []string{}
	if len(f.Questions) == 0 {
		rows = append(rows, theme.Quiet.Render(ansi.Truncate("No questions were provided. Submit to continue.", width, "…")))
	}
	target := 0
	for index, question := range f.Questions {
		if index == f.ReviewCursor {
			target = len(rows)
		}
		prefix, style := "  ", theme.Body
		if index == f.ReviewCursor {
			prefix, style = "› ", theme.Accent
		}
		rows = append(rows, reviewRow(prefix, question.Field.Label, question.answerSummary(), width, style)...)
	}
	rows = append(rows, theme.Quiet.Render(strings.Repeat("─", min(width, 24))))
	if f.ReviewCursor >= len(f.Questions) {
		target = len(rows)
	}
	prefix, style := "  ", theme.Body
	if f.ReviewCursor >= len(f.Questions) {
		prefix, style = "› ", theme.Accent
	}
	rows = append(rows, style.Render(prefix+"✓ Submit answers"))
	return rows, target
}

func reviewRow(prefix, label, answer string, width int, style lipgloss.Style) []string {
	label, answer = oneLine(label), oneLine(answer)
	head := label + ": "
	if lipgloss.Width(prefix+head) >= width {
		head = ansi.Truncate(label, max(1, width-lipgloss.Width(prefix)-2), "…") + ": "
	}
	available := max(1, width-lipgloss.Width(prefix+head))
	lines := strings.Split(lipgloss.Wrap(answer, available, " "), "\n")
	rows := []string{style.Render(ansi.Truncate(prefix+head+lines[0], width, "…"))}
	indent := strings.Repeat(" ", lipgloss.Width(prefix+head))
	for _, line := range lines[1:] {
		rows = append(rows, style.Render(ansi.Truncate(indent+line, width, "…")))
	}
	return rows
}

func (question *Question) answerSummary() string {
	answered := question.answered()
	var labels []string
	for _, option := range question.Field.Options {
		if slices.Contains(question.Field.Selected, option.Value) {
			labels = append(labels, option.Label)
		}
	}
	if question.Other != nil {
		if text := strings.TrimSpace(question.Other.Input.Value()); text != "" {
			labels = append(labels, "Other: "+text)
		}
	}
	if len(question.Field.Options) == 0 {
		if text := strings.TrimSpace(question.Field.Input.Value()); text != "" {
			labels = append(labels, text)
		}
	}
	if len(labels) > 0 {
		return strings.Join(labels, ", ")
	}
	if answered {
		return "Answered"
	}
	if question.Field.Required {
		return "Required"
	}
	return "Skipped"
}

func optionMarker(question *Question, value string, selected bool) string {
	if question.Field.Type == "array" {
		if selected {
			return "[x]"
		}
		return "[ ]"
	}
	if selected {
		return "●"
	}
	return "○"
}

// oneLine collapses untrusted label text to a single visual row. theme.Clean
// preserves newlines and tabs for prose, but fixed rows such as titles, tabs,
// option labels, and review summaries must not consume extra height.
func oneLine(text string) string {
	return strings.Join(strings.Fields(theme.Clean(text)), " ")
}

func appendWrapped(rows []string, text string, width int, style lipgloss.Style) []string {
	text = theme.Clean(text)
	if text == "" {
		return rows
	}
	for _, line := range strings.Split(lipgloss.Wrap(text, max(1, width), " "), "\n") {
		rows = append(rows, style.Render(line))
	}
	return rows
}

func appendIndented(rows []string, text string, width, indent int, style lipgloss.Style) []string {
	text = theme.Clean(text)
	if text == "" {
		return rows
	}
	pad := strings.Repeat(" ", indent)
	for _, line := range strings.Split(lipgloss.Wrap(text, max(1, width-indent), " "), "\n") {
		rows = append(rows, style.Render(pad+line))
	}
	return rows
}

func styledLines(text string, width int, style lipgloss.Style) []string {
	var lines []string
	for _, line := range strings.Split(lipgloss.Wrap(theme.Clean(text), max(1, width), " "), "\n") {
		lines = append(lines, style.Render(line))
	}
	return lines
}

// fitInput resizes a text field and recomputes its horizontal scroll window.
// SetWidth alone leaves the previous offset, which clips the cursor after the
// form is resized or first rendered narrower than the input's construction size.
func fitInput(input *textinput.Model, width int) {
	input.SetWidth(max(1, width))
	input.SetValue(input.Value())
}

func (f *Form) footer(width int, scroll bool) []string {
	lines := styledLines(f.hint(scroll), width, theme.Quiet)
	maxLines := 3
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = ansi.Truncate(lines[maxLines-1], max(1, width-1), "") + "…"
	}
	return lines
}

func (f *Form) hint(scroll bool) string {
	var hint string
	switch {
	case f.Hint != "":
		hint = f.Hint
	case f.Review:
		hint = "↑↓ answer · Enter edit/submit · Ctrl+Enter send · Esc skip"
	case len(f.Questions) == 0:
		hint = "Ctrl+Enter continue · Esc skip"
	case f.Questions[f.Cursor].onOther():
		hint = "Type an answer · Enter save · Esc back · Tab next question"
	case len(f.Questions[f.Cursor].Field.Options) == 0:
		if len(f.Questions) == 1 {
			hint = "Enter send · Esc skip"
		} else if f.returnToReview {
			hint = "Enter save · Tab next question · Ctrl+Enter send · Esc skip"
		} else {
			hint = "Enter next · Tab next question · Ctrl+Enter send · Esc skip"
		}
	case f.Questions[f.Cursor].Field.Type == "array":
		if len(f.Questions) == 1 {
			hint = "↑↓ choose · Space toggle · Enter send · Esc skip"
		} else {
			hint = "↑↓ choose · Space toggle · Tab next question · " + f.advanceHint() + " · Esc skip"
		}
	default:
		if len(f.Questions) == 1 {
			hint = "↑↓ choose · Space select · Enter send · Esc skip"
		} else {
			hint = "↑↓ choose · Space select · Tab next question · " + f.advanceHint() + " · Esc skip"
		}
	}
	if scroll {
		hint += " · PgUp/PgDn scroll"
	}
	return hint
}

func (f *Form) advanceHint() string {
	if f.returnToReview {
		return "Enter save · Ctrl+Enter send"
	}
	if f.Cursor == len(f.Questions)-1 {
		return "Enter review · Ctrl+Enter send"
	}
	return "Enter next · Ctrl+Enter send"
}
