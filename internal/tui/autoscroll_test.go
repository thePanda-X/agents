package tui

import (
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
)

// seedScrollableSession builds a session with enough history that the
// viewport can scroll, and leaves the model attached at the bottom.
func seedScrollableSession(t *testing.T) *Model {
	t.Helper()
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: strings.Repeat("A line of response.\n\n", 100)}}
	m.refresh()
	if !m.viewport.AtBottom() || !m.following {
		t.Fatalf("fixture must start attached at the bottom: following=%v atbottom=%v", m.following, m.viewport.AtBottom())
	}
	return m
}

func appendAssistant(m *Model, text string) {
	m.state.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{ID: "reply", Role: "assistant", Text: text}})
	m.Update(tickMsg{})
}

func TestAutoscrollDetachesOnScrollUpAndReattachesOnScrollDown(t *testing.T) {
	m := seedScrollableSession(t)
	// Scrolling up detaches.
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if m.following {
		t.Fatal("wheel up did not detach")
	}
	offset := m.viewport.YOffset()
	// Streaming must not move a detached reader.
	before := m.viewport.TotalLineCount()
	appendAssistant(m, strings.Repeat("more lines\n\n", 10))
	if m.viewport.YOffset() != offset {
		t.Fatalf("streaming moved the detached reader: got %d want %d", m.viewport.YOffset(), offset)
	}
	if m.viewport.TotalLineCount() <= before {
		t.Fatal("fixture did not actually grow")
	}
	// Scrolling back down to the bottom reattaches.
	for i := 0; i < 100 && !m.viewport.AtBottom(); i++ {
		m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	}
	if !m.following || !m.viewport.AtBottom() {
		t.Fatalf("returning to the bottom did not reattach: following=%v atbottom=%v", m.following, m.viewport.AtBottom())
	}
	pinned := m.viewport.YOffset()
	appendAssistant(m, "tail")
	if m.viewport.YOffset() != pinned || !m.viewport.AtBottom() {
		t.Fatalf("reattached reader did not follow streaming: got %d want %d", m.viewport.YOffset(), pinned)
	}
}

func TestAutoscrollBrowseKeysDetachAndReattach(t *testing.T) {
	m := seedScrollableSession(t)
	m.opts.Settings.VimNavigation = true
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(letter('k'))
	if m.following {
		t.Fatal("browse k above the bottom did not detach")
	}
	// j back to the bottom reattaches.
	for i := 0; i < 50 && !m.viewport.AtBottom(); i++ {
		m.Update(letter('j'))
		m.Update(tickMsg{})
	}
	if !m.following || !m.viewport.AtBottom() {
		t.Fatalf("browse j did not reattach: following=%v atbottom=%v", m.following, m.viewport.AtBottom())
	}
	// gg detaches at the top; G reattaches at the bottom.
	m.Update(letter('g'))
	m.Update(letter('g'))
	if m.following || m.viewport.YOffset() != 0 {
		t.Fatalf("gg did not detach at the top: following=%v offset=%d", m.following, m.viewport.YOffset())
	}
	m.Update(letter('G'))
	if !m.following || !m.viewport.AtBottom() {
		t.Fatalf("G did not reattach: following=%v atbottom=%v", m.following, m.viewport.AtBottom())
	}
}

func TestAutoscrollShortcutsAttachAndDetach(t *testing.T) {
	m := seedScrollableSession(t)
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if m.following || m.viewport.YOffset() != 0 {
		t.Fatalf("Ctrl+Home did not detach: following=%v offset=%d", m.following, m.viewport.YOffset())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if !m.following || !m.viewport.AtBottom() {
		t.Fatalf("Ctrl+End did not reattach: following=%v atbottom=%v", m.following, m.viewport.AtBottom())
	}
}

func TestAutoscrollSendingAPromptReattaches(t *testing.T) {
	m := seedScrollableSession(t)
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if m.following {
		t.Fatal("wheel up did not detach")
	}
	m.input.SetValue("follow me")
	m.send()
	if !m.following || !m.viewport.AtBottom() {
		t.Fatalf("sending did not reattach: following=%v atbottom=%v", m.following, m.viewport.AtBottom())
	}
}
