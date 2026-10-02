package overlays

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"agents/internal/tui/theme"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// View pages one schema field at a time, with bounded scrolling for long
// questions, option lists, previews, and the final response review.
func (f *Form) View(width, height int) string {
	width, height = max(1, width), max(1, height)
	var rows []string
	add := func(text string) { rows = append(rows, strings.Split(lipgloss.Wrap(text, width, " "), "\n")...) }
	target := 0
	if f.Review {
		add(theme.Heading.Render("Review your response"))
		for _, field := range f.Fields {
			if value, ok := f.Content[field.Name]; ok {
				data, _ := json.Marshal(value)
				add(theme.Body.Render(theme.Clean(field.Label) + ": " + theme.Clean(string(data))))
			}
		}
		if len(f.Content) == 0 {
			add(theme.Quiet.Render("No answers selected. Submit to leave optional fields unanswered."))
		}
	} else {
		if f.Message != "" {
			add(theme.Body.Render(theme.Clean(f.Message)))
			add("")
		}
		if len(f.Fields) == 0 {
			add(theme.Quiet.Render("No fields requested. Ctrl+S to review."))
		} else {
			field := &f.Fields[f.Cursor]
			label := fmt.Sprintf("Field %d/%d · %s", f.Cursor+1, len(f.Fields), theme.Clean(field.Label))
			if field.Required {
				label += " *"
			}
			add(theme.Heading.Render(label))
			if field.Description != "" {
				add(theme.Body.Render(theme.Clean(field.Description)))
			}
			add("")
			target = len(rows)
			if len(field.Options) > 0 {
				selected := field.selectedValues()
				for index, option := range field.Options {
					mark := "( )"
					if field.Type == "array" {
						mark = "[ ]"
					}
					if slices.Contains(selected, option.Value) {
						mark = "(x)"
						if field.Type == "array" {
							mark = "[x]"
						}
					}
					row := "  " + mark + " " + theme.Clean(option.Label)
					if index == field.ChoiceCursor {
						target = len(rows)
						row = theme.Accent.Render("› " + mark + " " + theme.Clean(option.Label))
					}
					add(row)
				}
				option := field.Options[field.ChoiceCursor]
				if option.Description != "" {
					add("")
					add(theme.Quiet.Render(theme.Clean(option.Description)))
				}
				if option.Preview != "" {
					add("")
					add(theme.Body.Render(theme.Clean(option.Preview)))
				}
			} else if field.Type == "boolean" {
				mark := "[ ]"
				if field.Input.Value() == "true" {
					mark = "[x]"
				}
				add(theme.Accent.Render(mark + " Yes · Space to toggle"))
			} else {
				field.Input.SetWidth(max(1, width-2))
				add(field.Input.View())
				if field.Type == "array" {
					add(theme.Quiet.Render("Enter a JSON array of strings."))
				}
			}
		}
	}
	if f.Error != "" {
		add("")
		add(theme.Error.Render(theme.Clean(f.Error)))
	}
	hints := "Ctrl+S review · Alt+S skip all · Esc cancel\nTab/Shift+Tab fields · ↑↓ move · Space/Enter select · Del clear · PgUp/PgDn scroll"
	if f.Review {
		hints = "PgUp/PgDn scroll · Ctrl+S send\nTab edit · Alt+S skip all · Esc cancel"
	}
	footer := strings.Split(lipgloss.Wrap(theme.Quiet.Render(hints), width, " "), "\n")
	// Reserve at least one content row, even in very short terminals.
	footer = footer[:min(len(footer), max(0, min(3, height-1)))]
	f.pageRows = max(1, height-len(footer))
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
	visible = append(visible, footer...)
	for index, line := range visible {
		visible[index] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(visible, "\n")
}
