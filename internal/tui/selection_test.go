package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"agents/internal/app"
	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func responseTextMouse(t *testing.T, m *Model, text string) tea.Mouse {
	t.Helper()
	x, y := m.transcriptOrigin()
	for row, line := range strings.Split(m.transcriptContent, "\n") {
		plain := ansi.Strip(line)
		if index := strings.Index(plain, text); index >= 0 {
			return tea.Mouse{X: x + ansi.StringWidth(plain[:index]) - m.viewport.XOffset(), Y: y + row - m.viewport.YOffset(), Button: tea.MouseLeft}
		}
	}
	t.Fatalf("rendered transcript missing %q: %q", text, ansi.Strip(m.transcriptContent))
	return tea.Mouse{}
}

func dragResponseText(t *testing.T, m *Model, text string, reverse bool) {
	t.Helper()
	start := responseTextMouse(t, m, text)
	end := start
	end.X += ansi.StringWidth(text) - 1
	if reverse {
		start, end = end, start
	}
	m.Update(tea.MouseClickMsg(start))
	m.Update(tea.MouseMotionMsg(end))
	m.Update(tea.MouseReleaseMsg(end))
	if !m.hasTextSelection() {
		t.Fatal("drag did not create a text selection")
	}
}

func requireClipboardText(t *testing.T, cmd tea.Cmd, want string) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no clipboard command returned")
	}
	// SetClipboard returns Bubble Tea's private OSC52 message (a string type).
	if got := fmt.Sprint(cmd()); got != want {
		t.Fatalf("clipboard got %q, want %q", got, want)
	}
}

func TestMouseTextSelectionAndCopyInResponsiveLayouts(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, focused := range []bool{false, true} {
			for _, size := range [][2]int{{40, 14}, {80, 24}, {120, 40}} {
				t.Run(fmt.Sprintf("compact=%v/browse=%v/%dx%d", compact, focused, size[0], size[1]), func(t *testing.T) {
					m := testModel(t)
					m.opts.Settings.Compact = compact
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "Before **highlighted** after."}}
					m.input.SetValue("keep draft\nand cursor")
					m.resize()
					m.setTranscriptFocus(focused)
					m.refresh()
					before := m.input.Value()
					mouse := responseTextMouse(t, m, "highlighted")
					// The hit-testing origin must match the actual full-screen view.
					line := strings.Split(ansi.Strip(m.View().Content), "\n")[mouse.Y]
					if !strings.HasPrefix(ansi.Cut(line, mouse.X, ansi.StringWidth(line)), "highlighted") {
						t.Fatalf("mouse coordinates do not match the view: %q at %v", line, mouse)
					}
					dragResponseText(t, m, "highlighted", true)
					if got := m.textSelection.Text(m.transcriptContent); got != "highlighted" {
						t.Fatalf("selected %q", got)
					}
					if !strings.Contains(ansi.Strip(m.footer(m.viewport.Width())), "y copy") && !strings.Contains(ansi.Strip(m.footer(m.viewport.Width())), "right-click copy") {
						t.Fatal("selection copy hint missing")
					}
					_, cmd := m.Update(letter('y'))
					requireClipboardText(t, cmd, "highlighted")
					_, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
					requireClipboardText(t, cmd, "highlighted")
					mouse.Button = tea.MouseRight
					_, cmd = m.Update(tea.MouseClickMsg(mouse))
					requireClipboardText(t, cmd, "highlighted")
					if m.input.Value() != before || m.focusedTranscript != focused {
						t.Fatal("selecting or copying changed the draft or keyboard focus")
					}
					if !strings.Contains(ansi.Strip(m.feedback(100)), "✓ Selection copy requested") {
						t.Fatal("copy toast missing")
					}
					if width, height := lipgloss.Size(m.View().Content); width > size[0] || height > size[1] {
						t.Fatal("selection UI overflowed terminal")
					}
				})
			}
		}
	}
}

func selectionModel(t *testing.T) *Model {
	t.Helper()
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "Before selected text after."}}
	m.refresh()
	dragResponseText(t, m, "selected text", false)
	return m
}

func TestTextSelectionTracksAppearanceWithoutChangingTextOrLayout(t *testing.T) {
	m := selectionModel(t)
	plain := ansi.Strip(m.View().Content)
	for _, test := range []struct {
		name       string
		appearance string
		background string
	}{
		{"graphite", "", "48;2;73;51;64"},
		{"high-contrast", "high-contrast", "48;2;96;62;81"},
		{"terminal", "terminal", ""},
		{"back-to-graphite", "", "48;2;73;51;64"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m.opts.Settings.Appearance = test.appearance
			m.refresh()
			view := m.View()
			if !m.hasTextSelection() || ansi.Strip(view.Content) != plain {
				t.Fatal("changing appearance lost the selection or changed the layout")
			}
			mouse := responseTextMouse(t, m, "selected text")
			line := strings.Split(view.Content, "\n")[mouse.Y]
			selected := ansi.Cut(line, mouse.X, mouse.X+len("selected text"))
			if test.appearance == "terminal" {
				if !strings.Contains(selected, "\x1b[7mselected text") || strings.Contains(selected, "38;") || strings.Contains(selected, "48;") {
					t.Fatalf("terminal selection lost reverse video or retained colors: %q", selected)
				}
			} else if !strings.Contains(selected, test.background) {
				t.Fatalf("selection does not use its themed background: %q", selected)
			}
			if got := m.textSelection.Text(m.transcriptContent); got != "selected text" {
				t.Fatalf("changing appearance changed selected text: %q", got)
			}
		})
	}
	requireClipboardText(t, m.copyTextSelection(), "selected text")
}

func TestSelectionClearsWithoutConsumingDraftEditing(t *testing.T) {
	m := selectionModel(t)
	m.input.Focus()
	m.input.SetValue("draft")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.textSelection != nil || m.focusedTranscript || m.input.Value() != "draft" {
		t.Fatal("Esc did not clear only the text selection")
	}
	dragResponseText(t, m, "selected text", false)
	m.Update(letter('x'))
	if m.textSelection != nil || m.input.Value() != "draftx" {
		t.Fatalf("typing did not resume draft editing: selection=%v draft=%q", m.textSelection, m.input.Value())
	}
	m.Update(letter('y'))
	if m.input.Value() != "draftxy" {
		t.Fatal("y in the composer was intercepted without a selection")
	}
	dragResponseText(t, m, "selected text", false)
	click := responseTextMouse(t, m, "Before")
	m.Update(tea.MouseClickMsg(click))
	m.Update(tea.MouseReleaseMsg(click))
	if m.textSelection != nil {
		t.Fatal("a plain click became a text selection")
	}
	dragResponseText(t, m, "selected text", false)
	m.Update(tea.PasteMsg{Content: " pasted text"})
	if m.textSelection != nil || !strings.HasSuffix(m.input.Value(), " pasted text") {
		t.Fatal("pasting did not resume ordinary draft editing")
	}
}

func TestTextSelectionDoesNotOverrideKeysOrModalInput(t *testing.T) {
	m := selectionModel(t)
	m.opts.Settings.Keybindings = map[string]string{"palette": "y"}
	m.Update(letter('y'))
	if m.overlay != "palette" {
		t.Fatal("selection ignored the explicit y remap")
	}
	m.closeOverlay()
	m.opts.Settings.Keybindings = nil
	dragResponseText(t, m, "selected text", false)
	m.openSettings()
	mouse := responseTextMouse(t, m, "selected text")
	mouse.Button = tea.MouseRight
	if cmd := m.handleMouse(tea.MouseClickMsg(mouse)); cmd != nil {
		t.Fatal("right-click copied transcript text through an overlay")
	}
	m.closeOverlay()
	m.opts.Settings.Mouse = false
	if cmd := m.handleMouse(tea.MouseClickMsg(mouse)); cmd != nil {
		t.Fatal("mouse copying ignored the disabled mouse setting")
	}
	// Ctrl+C keeps its existing cancellation/draft-clearing precedence.
	m.input.SetValue("draft to preserve")
	m.state.Active().Busy = true
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil || !m.state.Active().Cancelling || m.input.Value() != "draft to preserve" {
		t.Fatal("selection broke Ctrl+C cancellation precedence")
	}
}

func TestTextSelectionInvalidatesOnReflowOrConversationSwitch(t *testing.T) {
	for _, change := range []string{"resize", "compact", "content", "conversation"} {
		t.Run(change, func(t *testing.T) {
			m := selectionModel(t)
			switch change {
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
			case "compact":
				m.toggleCompact()
			case "content":
				m.state.Active().Entries[0].Text = "Replacement response"
				m.state.Active().Entries[0].Revision++
				m.refresh()
			case "conversation":
				m.state.Conversations["other"] = &app.Conversation{Entries: []*app.Entry{{Role: "assistant", Text: "Other response"}}}
				m.state.ActiveID = "other"
				if m.hasTextSelection() {
					t.Fatal("old conversation selection is still copyable")
				}
				m.refresh()
			}
			if m.textSelection != nil {
				t.Fatal("selection survived an invalidating layout/content change")
			}
		})
	}
}

func TestTextSelectionSurvivesStreamingElsewhereAndPausesFollowing(t *testing.T) {
	m := selectionModel(t)
	offset := m.viewport.YOffset()
	m.state.Active().Entries[0].Text += " More text."
	m.state.Active().Entries[0].Revision++
	m.refresh()
	if !m.hasTextSelection() {
		t.Fatal("appending outside the selected cells invalidated the selection")
	}
	m.state.Active().Entries = append(m.state.Active().Entries, &app.Entry{Role: "assistant", Text: strings.Repeat("Later response paragraph.\n\n", 80)})
	m.refresh()
	if !m.hasTextSelection() || m.viewport.YOffset() != offset {
		t.Fatal("streaming elsewhere moved or invalidated the selection")
	}
	requireClipboardText(t, m.copyTextSelection(), "selected text")
}

func TestMouseWheelExtendsSelectionAcrossPages(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "First selected row.\n\n" + strings.Repeat("More response rows.\n\n", 40)}}
	m.refresh()
	m.viewport.GotoTop()
	mouse := responseTextMouse(t, m, "First")
	m.Update(tea.MouseClickMsg(mouse))
	for range 10 {
		wheel := mouse
		wheel.Button = tea.MouseWheelDown
		m.Update(tea.MouseWheelMsg(wheel))
	}
	m.Update(tea.MouseReleaseMsg(mouse))
	if !m.hasTextSelection() || m.viewport.YOffset() == 0 || m.textSelection.Head.Row < m.viewport.Height() {
		t.Fatal("mouse wheel did not extend selection past the first page")
	}
	text := m.textSelection.Text(m.transcriptContent)
	if !strings.HasPrefix(text, "First selected row.\n") || strings.Count(text, "More response rows.") < 5 {
		t.Fatalf("offscreen selected text was lost: %q", text)
	}
	requireClipboardText(t, m.copyTextSelection(), text)
}

func TestCopyFallbacksAndRepeatedToast(t *testing.T) {
	m := selectionModel(t)
	m.noticeAt = time.Now().Add(-time.Minute)
	requireClipboardText(t, m.copyLast(), "selected text")
	if time.Since(m.noticeAt) > time.Second || m.feedback(100) == "" {
		t.Fatal("repeating copy did not refresh the toast")
	}
	m.clearTextSelection()
	requireClipboardText(t, m.copyLast(), "Before selected text after.")
	requireClipboardText(t, m.copySelected(), "Before selected text after.")
	mouse := responseTextMouse(t, m, "selected text")
	mouse.Button = tea.MouseRight
	if cmd := m.handleMouse(tea.MouseClickMsg(mouse)); cmd != nil {
		t.Fatal("right-click without a text selection copied an entire response")
	}
}

func TestYankCommandUsesHighlightedTextWithoutSendingTheDraft(t *testing.T) {
	m := selectionModel(t)
	m.input.SetValue("/yank")
	cmd, handled := m.handleSlashCommand(m.input.Value())
	if !handled {
		t.Fatal("yank command was not handled locally")
	}
	requireClipboardText(t, cmd, "selected text")
	if m.submitting || len(m.state.Active().Entries) != 1 {
		t.Fatal("yank submitted an agent prompt")
	}
}

func TestMouseSelectionHitsWrappedResponseAtScrolledOffset(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: strings.Repeat("Long wrapped response words. ", 30) + "selectme"}}
	m.refresh()
	m.viewport.GotoBottom()
	if m.viewport.YOffset() == 0 {
		t.Fatal("fixture did not scroll")
	}
	dragResponseText(t, m, "selectme", false)
	requireClipboardText(t, m.copyTextSelection(), "selectme")
}

func TestTextSelectionAccountsForHorizontalScrollAndLegacyRelease(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	m.state.Active().Entries = []*app.Entry{{Role: "assistant"}}
	// Supply an over-wide rendered row to exercise the viewport's x offset.
	m.transcriptContent = "  " + strings.Repeat("a", 45) + "target ending"
	m.setTranscriptContent()
	m.viewport.SetXOffset(30)
	m.dirty = false
	mouse := responseTextMouse(t, m, "target")
	end := mouse
	end.X += len("target") - 1
	m.Update(tea.MouseClickMsg(mouse))
	m.Update(tea.MouseMotionMsg(end))
	end.Button = tea.MouseNone
	m.Update(tea.MouseReleaseMsg(end))
	if !m.hasTextSelection() || m.textSelection.Head != (transcript.TextPosition{Row: 0, Column: 52}) || m.textSelection.dragging {
		t.Fatal("horizontal scroll or legacy release changed the selection")
	}
	requireClipboardText(t, m.copyTextSelection(), "target")
}
