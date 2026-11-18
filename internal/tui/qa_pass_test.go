package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/tui/overlays"
	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func enterKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func TestInfoPagerHintListsOnlyApplicableKeys(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state.Active().Plan = []harness.PlanEntry{{Content: "Read", Status: "in_progress"}}
	m.perform("plan")
	_, width, _ := m.modalLayout()
	hint := m.infoHint(width)
	if hint != "Esc close" {
		t.Fatalf("short plan advertises keys that do nothing: %q", hint)
	}
	if m.info.Height() > 3 {
		t.Fatalf("short content kept a full-height pager: %d rows", m.info.Height())
	}
	if !strings.Contains(ansi.Strip(m.infoContent), "in progress") {
		t.Fatal("plan status was not humanized")
	}

	m.closeOverlay()
	m.perform("help")
	if hint := m.infoHint(width); !strings.Contains(hint, "j/k scroll") || strings.Contains(hint, "J/K hunks") || strings.Contains(hint, "e open file") {
		t.Fatalf("long help hint: %q", hint)
	}
	if footer := ansi.Strip(m.footer(96)); !strings.Contains(footer, "j/k:scroll") {
		t.Fatalf("scrollable pager footer: %q", footer)
	}
}

func TestInfoOpenedFromPickerSaysEscGoesBack(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state.Active().Entries = []*app.Entry{{ID: "t", Role: "tool", Tool: &app.ToolState{Title: "Edit", Status: "completed", Diffs: []harness.Diff{{Path: "/workspace/a.go", OldText: "a\n", NewText: "b\n"}}}}}
	m.perform("changes")
	if item, ok := m.list.Selected(); !ok || item.Label != "a.go" || item.Detail != "completed" {
		t.Fatalf("changes item should be workspace-relative with its status: %+v", item)
	}
	if !strings.Contains(m.list.Description, "1 file · 1 report.") {
		t.Fatalf("changes summary is not pluralized correctly: %q", m.list.Description)
	}
	m.handleKey(enterKey())
	_, width, _ := m.modalLayout()
	if hint := m.infoHint(width); !strings.Contains(hint, "Esc back") || !strings.Contains(hint, "e open file") {
		t.Fatalf("diff pager hint: %q", hint)
	}
	if footer := ansi.Strip(m.footer(96)); !strings.Contains(footer, "Esc:back") {
		t.Fatalf("diff pager footer: %q", footer)
	}
}

func TestAbsoluteDiffPathsHaveNoDoubledSlash(t *testing.T) {
	out := ansi.Strip(transcript.RenderDiff(harness.Diff{Path: "/workspace/a.go", OldText: "a\n", NewText: "b\n"}))
	if strings.Contains(out, "a//") || !strings.Contains(out, "--- /workspace/a.go") {
		t.Fatalf("absolute diff header: %q", out)
	}
	out = ansi.Strip(transcript.RenderDiff(harness.Diff{Path: "src/a.go", OldText: "a\n", NewText: "b\n"}))
	if !strings.Contains(out, "--- a/src/a.go") || !strings.Contains(out, "+++ b/src/a.go") {
		t.Fatalf("relative diff header: %q", out)
	}
}

func TestQueryArgumentsPrefillPickerFilters(t *testing.T) {
	m := testModel(t)
	m.state.Active().Entries = []*app.Entry{{Role: "user", Text: "parser"}, {Role: "assistant", Text: "other"}}
	m.input.SetValue("/search parser")
	m.handleKey(enterKey())
	if m.overlay != "search" || m.list.Search.Value() != "parser" || len(m.list.Filtered()) != 1 {
		t.Fatalf("/search query did not prefill the filter: overlay=%q", m.overlay)
	}
	if m.input.Value() != "" {
		t.Fatal("local command text stayed in the composer")
	}
	m.closeOverlay()
	m.input.SetValue("/help me")
	m.handleKey(enterKey())
	if m.state.LastError == nil || m.overlay != "" {
		t.Fatal("argument-free command accepted arguments")
	}
}

func TestCloseCommandIsDiscoverableAndGivesFeedback(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.Close = true
	if command, ok := findLocalCommand("close"); !ok || command.Action != "close_session" {
		t.Fatal("/close does not map to closing the session")
	}
	m.openPalette()
	for _, item := range m.list.Items {
		if item.ID == "close_session" && item.Detail != "/close" {
			t.Fatalf("palette detail for close: %q", item.Detail)
		}
	}
	m.closeOverlay()
	m.input.SetValue("/close")
	if cmd := m.handleKey(enterKey()); cmd == nil || m.state.Notice != "Closing session…" {
		t.Fatalf("close gave no feedback: %q", m.state.Notice)
	}
}

func TestEmptyStatesExplainThemselves(t *testing.T) {
	m := testModel(t)
	m.perform("clear_attachments")
	if m.state.Notice != "No pending attachments to remove" {
		t.Fatalf("detach with nothing attached: %q", m.state.Notice)
	}
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "no code"}}
	m.perform("code")
	if !strings.Contains(m.list.EmptyMessage, "no fenced code blocks") {
		t.Fatal("code picker lacks an empty-state explanation")
	}
	m.closeOverlay()
	m.input.SetValue("/agent")
	m.handleKey(enterKey())
	if !strings.Contains(m.list.EmptyMessage, "has not advertised commands") {
		t.Fatalf("agent command picker empty state: %q", m.list.EmptyMessage)
	}
}

func TestThoughtsToggleAnnouncesNewState(t *testing.T) {
	m := testModel(t)
	m.perform("thoughts")
	if m.state.Notice != "Reasoning expanded by default" {
		t.Fatalf("notice: %q", m.state.Notice)
	}
	m.perform("thoughts")
	if m.state.Notice != "Reasoning collapsed by default" {
		t.Fatalf("notice: %q", m.state.Notice)
	}
}

func TestPickerFooterNamesEnterAction(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	for action, want := range map[string]string{"settings": "Enter:toggle", "commands": "Enter:insert", "palette": "Enter:run", "attachments": "Del:remove"} {
		m.closeOverlay()
		m.perform(action)
		if footer := ansi.Strip(m.footer(116)); !strings.Contains(footer, want) {
			t.Fatalf("%s footer %q lacks %q", action, footer, want)
		}
	}
}

func TestRenameFormUsesClientWording(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.renameSession(overlays.Item{Value: harness.SessionInfo{ID: "s", Title: "Title"}})
	view := ansi.Strip(m.form.View(80, 20))
	if !strings.Contains(view, "Rename session") || !strings.Contains(view, "Enter save · Esc cancel") || strings.Contains(view, "Question") || strings.Contains(view, "skip") {
		t.Fatalf("rename form wording: %q", view)
	}
}

func TestEmptyFreeTextAnswerIsVisible(t *testing.T) {
	form, err := overlays.NewForm(json.RawMessage(`{"type":"object","properties":{"notes":{"type":"string","title":"Notes"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if view := ansi.Strip(form.View(80, 12)); !strings.Contains(view, "Type your answer…") {
		t.Fatalf("empty text field is invisible: %q", view)
	}
}
