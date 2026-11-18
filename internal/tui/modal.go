package tui

import (
	"strings"

	"agents/internal/tui/theme"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The same content budgets are used by viewport sizing and panel rendering.
// Short terminals trade padding for visible choices and dismissal instructions.
func (m *Model) modalLayout() (lipgloss.Style, int, int) {
	style := theme.Panel
	outerWidth, outerHeight := min(106, m.width-6), m.height-4
	if m.width < 60 || m.height < 20 {
		style = style.Padding(0, 1)
		outerWidth, outerHeight = min(106, m.width-2), m.height-2
	}
	width := max(12, outerWidth-style.GetHorizontalFrameSize())
	height := max(4, outerHeight-style.GetVerticalFrameSize())
	return style.Width(width + style.GetHorizontalFrameSize()), width, height
}

func modalTitle(title string, width int) string {
	return theme.Heading.Render(ansi.Truncate(strings.Join(strings.Fields(theme.Clean(title)), " "), width, "…"))
}

func (m *Model) interactionLayout(height int) (choices, details, gaps int) {
	if m.height >= 20 {
		gaps = 2
	}
	choices = min(len(m.list.Items), max(1, height-4-gaps))
	details = max(1, height-1-choices-2-gaps)
	return
}

func (m *Model) modalContent(width, height int) string {
	if m.form != nil {
		return m.form.View(width, height)
	}
	if m.interaction != nil && m.list != nil {
		choices, _, gaps := m.interactionLayout(height)
		parts := []string{modalTitle(m.list.Title, width)}
		if gaps > 0 {
			parts = append(parts, "")
		}
		parts = append(parts, m.info.View())
		if gaps > 0 {
			parts = append(parts, "")
		}
		start := min(max(0, m.list.Cursor-choices+1), max(0, len(m.list.Items)-choices))
		for index := start; index < min(len(m.list.Items), start+choices); index++ {
			prefix := "  "
			style := theme.Body
			if index == m.list.Cursor {
				prefix, style = "› ", theme.Accent
			}
			label := strings.Join(strings.Fields(theme.Clean(m.list.Items[index].Label)), " ")
			parts = append(parts, style.Render(prefix+ansi.Truncate(label, width-2, "…")))
		}
		hint := "PgUp/PgDn details · ↑↓ choose"
		if width < 40 {
			hint = "PgDn details · ↑↓ choose"
		}
		if m.interaction.Kind == "url" && !m.urlReviewed {
			hint = "PgDn to read complete URL"
		}
		parts = append(parts, theme.Quiet.Render(ansi.Truncate(hint, width, "…")), theme.Quiet.Render("Enter confirm · Esc cancel"))
		return strings.Join(parts, "\n")
	}
	if m.list != nil {
		return m.list.View(width, height)
	}
	return modalTitle(m.infoTitle, width) + "\n" + m.info.View() + "\n" + theme.Quiet.Render(ansi.Truncate(m.infoHint(width), width, "…"))
}

// infoHint lists only the pager keys that do something for the open content:
// scrolling when it overflows, hunk jumps for multi-hunk diffs, and e for
// reported files. Esc reads "back" when it returns to a parent picker.
func (m *Model) infoHint(width int) string {
	var parts []string
	if m.info.TotalLineCount() > m.info.Height() {
		if width < 60 {
			parts = append(parts, "j/k scroll")
		} else {
			parts = append(parts, "j/k scroll · Ctrl+D/U page")
		}
	}
	if len(m.infoHunks) > 1 && width >= 60 {
		parts = append(parts, "J/K hunks")
	}
	if len(m.infoPaths) > 0 {
		parts = append(parts, "e open file")
	}
	if len(m.overlayStack) > 0 {
		parts = append(parts, "Esc back")
	} else {
		parts = append(parts, "Esc close")
	}
	return strings.Join(parts, " · ")
}

func (m *Model) sizeInfo() {
	_, width, height := m.modalLayout()
	m.info.SetWidth(width)
	if m.interaction != nil && m.list != nil && m.form == nil {
		_, details, _ := m.interactionLayout(height)
		wrapped := lipgloss.Wrap(theme.Clean(m.info.GetContent()), width, " ")
		m.info.SetHeight(min(details, max(1, len(strings.Split(wrapped, "\n")))))
	} else {
		// Short content (plans, notices, diagnostics) gets a short pager instead
		// of a mostly blank full-height panel; long content still scrolls.
		rows := height - 2
		if m.infoContent != "" {
			rows = min(rows, strings.Count(lipgloss.Wrap(m.infoContent, width, " "), "\n")+1)
		}
		m.info.SetHeight(max(1, rows))
	}
}
