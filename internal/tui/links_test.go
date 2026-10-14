package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func fakeBrowser(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake browser uses a POSIX executable")
	}
	dir := t.TempDir()
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	log := filepath.Join(dir, "opened-url")
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$AGENTS_TEST_BROWSER_LOG\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("AGENTS_TEST_BROWSER_LOG", log)
	return log
}

func TestClickLinkOpensBrowserWithoutChangingDraftOrFocus(t *testing.T) {
	log := fakeBrowser(t)
	target := "https://example.com/a?x=1&y=2#section"
	for _, compact := range []bool{false, true} {
		for _, focused := range []bool{false, true} {
			for _, appearance := range []string{"", "terminal", "high-contrast"} {
				for _, size := range [][2]int{{40, 14}, {80, 24}} {
					t.Run(fmt.Sprintf("compact=%v/browse=%v/%s/%dx%d", compact, focused, appearance, size[0], size[1]), func(t *testing.T) {
						m := testModel(t)
						m.opts.Settings.Compact, m.opts.Settings.Appearance = compact, appearance
						m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
						m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "Before [open link](" + target + ") after."}}
						m.input.SetValue("keep draft")
						m.attachments = []harness.Attachment{{Name: "keep.txt"}}
						m.setTranscriptFocus(focused)
						m.resize()
						m.refresh()
						m.dirty = false
						mouse := responseTextMouse(t, m, "open link")
						line := strings.Split(ansi.Strip(m.View().Content), "\n")[mouse.Y]
						if !strings.HasPrefix(ansi.Cut(line, mouse.X, ansi.StringWidth(line)), "open link") {
							t.Fatal("link coordinates do not match the visible view")
						}
						beforeOffset := m.viewport.YOffset()
						if _, cmd := m.Update(tea.MouseClickMsg(mouse)); cmd != nil {
							t.Fatal("link opened on press, before a possible drag")
						}
						_, cmd := m.Update(tea.MouseReleaseMsg(mouse))
						if cmd == nil {
							t.Fatal("link click returned no browser command")
						}
						msg := cmd().(linkOpenedMsg)
						if msg.err != nil {
							t.Fatal(msg.err)
						}
						m.Update(msg)
						data, err := os.ReadFile(log)
						if err != nil || string(data) != target+"\n" {
							t.Fatalf("browser arguments = %q, err = %v", data, err)
						}
						if m.input.Value() != "keep draft" || len(m.attachments) != 1 || m.focusedTranscript != focused || m.textSelection != nil || m.viewport.YOffset() != beforeOffset {
							t.Fatal("opening a link disturbed the draft, attachments, focus, selection, or reading position")
						}
					})
				}
			}
		}
	}
}

func TestLinkDragSelectsInsteadOfOpening(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "Before [select link](https://example.com) after."}}
	m.refresh()
	m.dirty = false
	start := responseTextMouse(t, m, "select link")
	end := start
	end.X += len("select link") - 1
	m.Update(tea.MouseClickMsg(start))
	m.Update(tea.MouseMotionMsg(end))
	if _, cmd := m.Update(tea.MouseReleaseMsg(end)); cmd != nil {
		t.Fatal("dragging over a link opened the browser")
	}
	if !m.hasTextSelection() || m.textSelection.Text(m.transcriptContent) != "select link" {
		t.Fatal("dragging a link did not select its visible text")
	}
	requireClipboardText(t, m.copyTextSelection(), "select link")
}

func TestLinkClicksRespectUIBoundariesAndStaleTargets(t *testing.T) {
	for _, change := range []string{"disabled", "overlay", "outside", "padding", "resize", "conversation", "changed target", "drag returns to start"} {
		t.Run(change, func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "[link](https://example.com)"}}
			m.refresh()
			m.dirty = false
			mouse := responseTextMouse(t, m, "link")
			if change == "padding" {
				mouse.X = m.width - 3
			}
			m.Update(tea.MouseClickMsg(mouse))
			switch change {
			case "disabled":
				m.opts.Settings.Mouse = false
			case "overlay":
				m.openSettings()
			case "outside":
				mouse.Y = 0
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
			case "conversation":
				m.state.Conversations["other"] = &app.Conversation{Entries: []*app.Entry{{Role: "assistant", Text: "[link](https://example.com)"}}}
				m.state.ActiveID = "other"
				m.dirty = true
			case "changed target":
				m.state.Active().Entries[0].Text = "[link](https://different.example)"
				m.dirty = true
			case "drag returns to start":
				end := mouse
				end.X++
				m.Update(tea.MouseMotionMsg(end))
				m.Update(tea.MouseMotionMsg(mouse))
			}
			if _, cmd := m.Update(tea.MouseReleaseMsg(mouse)); cmd != nil {
				t.Fatal("opened a link after an invalidating interaction")
			}
		})
	}
}

func TestLinkAtAccountsForScrollingAndLegacyRelease(t *testing.T) {
	log := fakeBrowser(t)
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	m.state.Active().Entries = []*app.Entry{{Role: "assistant"}}
	target := "http://localhost:3000/path?q=hello&next=world"
	m.transcriptContent = strings.Repeat("  earlier\n", 30) + "  " + strings.Repeat("x", 45) + ansi.SetHyperlink(target) + "target" + ansi.ResetHyperlink()
	m.setTranscriptContent()
	m.viewport.GotoBottom()
	m.viewport.SetXOffset(30)
	m.dirty = false
	mouse := responseTextMouse(t, m, "target")
	m.Update(tea.MouseClickMsg(mouse))
	mouse.Button = tea.MouseNone
	_, cmd := m.Update(tea.MouseReleaseMsg(mouse))
	if cmd == nil {
		t.Fatal("scrolled legacy click missed the link")
	}
	if msg := cmd().(linkOpenedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	data, err := os.ReadFile(log)
	if err != nil || string(data) != target+"\n" {
		t.Fatalf("browser received %q, err = %v", data, err)
	}
}

func TestClickWrappedMarkdownURLUsesFullTarget(t *testing.T) {
	log := fakeBrowser(t)
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	target := "https://example.com/" + strings.Repeat("long-path/", 20) + "?one=1&two=2#section"
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "Read [docs](" + target + ")."}}
	m.refresh()
	m.dirty = false
	if m.viewport.YOffset() == 0 {
		t.Fatal("fixture did not wrap and scroll")
	}
	x, y := m.transcriptOrigin()
	for row, line := range strings.Split(m.transcriptContent, "\n") {
		if row <= m.viewport.YOffset() || row >= m.viewport.YOffset()+m.viewport.Height() {
			continue
		}
		// Hit a continuation of the URL, not its label or first line.
		plain := ansi.Strip(line)
		if index := strings.Index(plain, "long-path/"); index >= 0 {
			mouse := tea.Mouse{X: x + ansi.StringWidth(plain[:index]), Y: y + row - m.viewport.YOffset(), Button: tea.MouseLeft}
			m.Update(tea.MouseClickMsg(mouse))
			_, cmd := m.Update(tea.MouseReleaseMsg(mouse))
			if cmd == nil {
				t.Fatal("wrapped URL click returned no browser command")
			}
			if msg := cmd().(linkOpenedMsg); msg.err != nil {
				t.Fatal(msg.err)
			}
			data, err := os.ReadFile(log)
			if err != nil || string(data) != target+"\n" {
				t.Fatalf("browser lost full wrapped target: %q, err = %v", data, err)
			}
			return
		}
	}
	t.Fatal("fixture has no visible wrapped URL continuation")
}

func TestClickPlainURLInPromptOrToolOutput(t *testing.T) {
	log := fakeBrowser(t)
	target := "http://example.com/page"
	for _, role := range []string{"user", "tool"} {
		t.Run(role, func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			entry := &app.Entry{ID: "entry", Role: role, Text: "Read (" + target + ")."}
			if role == "tool" {
				entry.Tool = &app.ToolState{Title: "Result", Status: "completed", Text: entry.Text}
				m.expanded[entry.ID] = true
			}
			m.state.Active().Entries = []*app.Entry{entry}
			m.refresh()
			m.dirty = false
			mouse := responseTextMouse(t, m, target)
			m.Update(tea.MouseClickMsg(mouse))
			_, cmd := m.Update(tea.MouseReleaseMsg(mouse))
			if cmd == nil {
				t.Fatal("plain URL click returned no browser command")
			}
			if msg := cmd().(linkOpenedMsg); msg.err != nil {
				t.Fatal(msg.err)
			}
			data, err := os.ReadFile(log)
			if err != nil || string(data) != target+"\n" {
				t.Fatalf("browser arguments = %q, err = %v", data, err)
			}
		})
	}
}

func TestBrowserLaunchFailureIsReported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := testModel(t)
	msg := openLink(m.opts.Context, "https://example.com")().(linkOpenedMsg)
	if msg.err == nil {
		t.Fatal("missing browser opener was not reported")
	}
	m.Update(msg)
	if m.state.LastError == nil || !strings.Contains(m.state.LastError.Error(), "could not open link") {
		t.Fatal("browser failure did not reach UI feedback")
	}
}

func TestOpenLinkRejectsUnsafeTargetsWithoutLaunchingBrowser(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "javascript:alert(1)", "data:text/html,test", "mailto:person@example.com", "/relative/path", "//example.com", "https://", "https://user:secret@example.com", "https://example.com/\x1b", "https://example.com/\n", "https://example.com/%zz"} {
		t.Run(target, func(t *testing.T) {
			if msg := openLink(context.Background(), target)().(linkOpenedMsg); msg.err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
	for _, target := range []string{"http://example.com", "https://example.com/path?a=1&b=2", "http://localhost:3000", "http://[::1]:3000", "HTTPS://example.com"} {
		if err := validateWebURL(target); err != nil {
			t.Fatalf("valid URL %q: %v", target, err)
		}
	}
}

func TestLinkResultDoesNotAnswerAnACPInteraction(t *testing.T) {
	m := testModel(t)
	i := &harness.Interaction{ID: "url", Kind: "url", URL: "https://example.com", Reply: make(chan harness.Decision, 1)}
	m.state.Interactions = []*harness.Interaction{i}
	m.syncInteraction()
	m.urlOpening = true
	m.Update(linkOpenedMsg{})
	if m.interaction != i || !m.urlOpening || len(i.Reply) != 0 {
		t.Fatal("ordinary link result answered or changed an ACP interaction")
	}
	m.Update(linkOpenedMsg{errors.New("browser unavailable")})
	if m.state.LastError == nil || !strings.Contains(m.state.LastError.Error(), "browser unavailable") || m.interaction != i || len(i.Reply) != 0 {
		t.Fatal("browser failure was hidden or cancelled an unrelated interaction")
	}
}
