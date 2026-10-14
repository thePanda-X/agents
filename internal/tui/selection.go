package tui

import (
	"strings"

	"agents/internal/app"
	"agents/internal/tui/theme"
	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type textSelection struct {
	transcript.TextSelection
	conversation *app.Conversation
	width        int
	dragging     bool
	moved        bool
	link         string
}

// These coordinates match View's outer padding and header/blank-row layout.
func (m *Model) transcriptOrigin() (int, int) {
	if m.opts.Settings.Compact {
		return 2, 1
	}
	return 2, 2
}

func (m *Model) inTranscript(mouse tea.Mouse) bool {
	x, y := m.transcriptOrigin()
	return mouse.X >= x && mouse.X < x+m.viewport.Width() &&
		mouse.Y >= y && mouse.Y < y+m.viewport.Height()
}

func (m *Model) textPosition(mouse tea.Mouse) transcript.TextPosition {
	x, y := m.transcriptOrigin()
	lines := strings.Split(m.transcriptContent, "\n")
	row := min(max(0, m.viewport.YOffset()+min(max(0, mouse.Y-y), m.viewport.Height()-1)), len(lines)-1)
	column := m.viewport.XOffset() + min(max(0, mouse.X-x), m.viewport.Width()-1)
	column = min(max(2, column), max(2, ansi.StringWidth(lines[row])-1))
	return transcript.TextPosition{Row: row, Column: column}
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !m.opts.Settings.Mouse || m.width < 32 || m.height < 12 ||
		m.overlay != "" || m.list != nil || m.interaction != nil || m.form != nil {
		return nil
	}
	if m.dirty {
		m.refresh()
		m.dirty = false
	}
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		switch mouse.Button {
		case tea.MouseRight:
			if m.inTranscript(mouse) && m.hasTextSelection() {
				return m.copyTextSelection()
			}
		case tea.MouseLeft:
			m.clearTextSelection()
			_, top := m.transcriptOrigin()
			if !m.inTranscript(mouse) || m.transcriptContent == "" ||
				m.viewport.YOffset()+mouse.Y-top >= m.viewport.TotalLineCount() {
				return nil
			}
			position := m.textPosition(mouse)
			m.textSelection = &textSelection{
				TextSelection: transcript.TextSelection{Anchor: position, Head: position},
				conversation:  m.state.Active(), width: m.viewport.Width(), dragging: true,
				link: m.linkAt(mouse),
			}
		}
	case tea.MouseMotionMsg:
		if m.textSelection != nil && m.textSelection.dragging && mouse.Button == tea.MouseLeft {
			_, top := m.transcriptOrigin()
			if mouse.Y < top {
				m.viewport.ScrollUp(1)
			} else if mouse.Y >= top+m.viewport.Height() {
				m.viewport.ScrollDown(1)
			}
			m.extendTextSelection(mouse)
		}
	case tea.MouseReleaseMsg:
		// Legacy mouse reporting may identify releases without a button.
		if m.textSelection != nil && m.textSelection.dragging && (mouse.Button == tea.MouseLeft || mouse.Button == tea.MouseNone) {
			m.extendTextSelection(mouse)
			m.textSelection.dragging = false
			if !m.textSelection.moved {
				// Open only on release: dragging a link must still select text.
				// Recheck the target in case streaming changed it during the click.
				link := m.textSelection.link
				m.clearTextSelection()
				if link != "" && m.linkAt(mouse) == link {
					return openLink(m.opts.Context, link)
				}
			}
		}
	}
	return nil
}

func (m *Model) extendTextSelection(mouse tea.Mouse) {
	s := m.textSelection
	s.Head = m.textPosition(mouse)
	s.moved = s.moved || s.Head != s.Anchor
	m.setTranscriptContent()
}

func (m *Model) hasTextSelection() bool {
	return m.textSelection != nil && m.textSelection.moved &&
		m.textSelection.conversation == m.state.Active()
}

func (m *Model) clearTextSelection() {
	if m.textSelection != nil {
		m.textSelection = nil
		m.viewport.SetContent(m.transcriptContent)
	}
}

func (m *Model) setTranscriptContent() {
	content := m.transcriptContent
	if m.hasTextSelection() {
		style := theme.TextSelection
		if m.opts.Settings.Appearance == "terminal" {
			style = theme.TerminalTextSelection
		}
		content = m.textSelection.Highlight(content, style)
	}
	m.viewport.SetContent(content)
}

// Keep a selection through unrelated updates, but never copy different text
// after reflow, a fold, or a conversation switch. Styling/gutter changes alone
// do not invalidate it. Selection also pauses automatic following while reading.
func (m *Model) validateTextSelection(content string) {
	s := m.textSelection
	if s == nil {
		return
	}
	if s.conversation != m.state.Active() || s.width != m.viewport.Width() {
		m.textSelection = nil
		return
	}
	// Appending to the same rendered line is safe if the highlighted cells
	// still contain exactly the text the reader selected.
	if s.Text(m.transcriptContent) != s.Text(content) {
		m.textSelection = nil
	}
}

func (m *Model) copyTextSelection() tea.Cmd {
	if m.dirty {
		m.refresh()
		m.dirty = false
	}
	if !m.hasTextSelection() {
		return nil
	}
	text := m.textSelection.Text(m.transcriptContent)
	if strings.TrimSpace(text) == "" {
		m.state.Notice = "Select some response text to copy"
		return nil
	}
	return m.copyText(text, "Selection")
}
