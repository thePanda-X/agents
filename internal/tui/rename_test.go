package tui

import (
	"strings"
	"testing"

	"agents/internal/harness"
	"agents/internal/storage"
	"agents/internal/tui/overlays"

	tea "charm.land/bubbletea/v2"
)

func TestSessionLabelPrefersNicknameOverTitleAndID(t *testing.T) {
	m := testModel(t)
	key := storage.SessionKey("s", m.opts.Descriptor.ID)
	m.sessionNames[key] = "Nickname"
	if got := m.sessionLabel("s", "Agent title"); got != "Nickname" {
		t.Fatalf("nickname not preferred: %q", got)
	}
	if got := m.sessionLabel("s", ""); got != "Nickname" {
		t.Fatalf("nickname not used without title: %q", got)
	}
	delete(m.sessionNames, key)
	if got := m.sessionLabel("s", "Agent title"); got != "Agent title" {
		t.Fatalf("title not restored: %q", got)
	}
	if got := m.sessionLabel("s", ""); got != "s" {
		t.Fatalf("ID fallback missing: %q", got)
	}
}

func TestActiveSessionLabelShowsNickname(t *testing.T) {
	m := testModel(t)
	m.state.Active().Info.Title = "Agent title"
	if got := m.activeSessionLabel(); got != "Agent title" {
		t.Fatalf("title expected: %q", got)
	}
	m.sessionNames[storage.SessionKey("s", m.opts.Descriptor.ID)] = "Nickname"
	if got := m.activeSessionLabel(); got != "Nickname" {
		t.Fatalf("nickname expected: %q", got)
	}
}

func TestWindowTitleUsesNickname(t *testing.T) {
	m := testModel(t)
	m.state.Active().Info.Title = "Fix parser"
	m.sessionNames[storage.SessionKey("s", m.opts.Descriptor.ID)] = "My task"
	if title := m.windowTitle(); !strings.Contains(title, "My task") {
		t.Fatalf("window title: %q", title)
	}
}

func TestPickerUsesNickname(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.List = true
	m.sessionNames[storage.SessionKey("s", m.opts.Descriptor.ID)] = "Nickname"
	m.state.Sessions = []harness.SessionInfo{{ID: "s", Title: "Agent title", UpdatedAt: "2026-10-01T10:00:00Z"}}
	m.openSessions()
	m.populateSessions()
	item, _ := m.list.Selected()
	if item.Label != "Nickname" {
		t.Fatalf("picker label: %q", item.Label)
	}
}

func TestRenameSessionFlow(t *testing.T) {
	m := testModel(t)
	m.opts.Store = &storage.Store{Directory: t.TempDir()}
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{
		{ID: "s", Title: "Agent title", UpdatedAt: "2026-10-01T10:00:00Z"},
		{ID: "other", Title: "Other", UpdatedAt: "2026-10-02T10:00:00Z"},
	}
	m.openSessions()
	m.populateSessions()

	selectListID(m.list, "s")
	m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.overlay != "rename" || m.form == nil {
		t.Fatal("rename form did not open")
	}
	if got := m.form.Questions[0].Field.Input.Value(); got != "Agent title" {
		t.Fatalf("prefill: %q", got)
	}

	m.form.Questions[0].Field.Input.SetValue("My name")
	action, _ := m.form.Key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != overlays.ActionSubmit {
		t.Fatal("enter did not submit")
	}
	if cmd := m.applyRename(); cmd != nil {
		cmd()
	}

	key := storage.SessionKey("s", m.opts.Descriptor.ID)
	if got := m.sessionNames[key]; got != "My name" {
		t.Fatalf("nickname: %q", got)
	}
	if m.overlay != "sessions" {
		t.Fatal("not returned to picker")
	}
	item, _ := m.list.Selected()
	if item.Label != "My name" {
		t.Fatalf("label after rename: %q", item.Label)
	}

	settings, snapshot := m.Snapshot()
	if snapshot.SessionNames[key] != "My name" {
		t.Fatal("snapshot missing nickname")
	}
	if err := m.opts.Store.Save(settings, snapshot); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := m.opts.Store.Load()
	if err != nil || loaded.SessionNames[key] != "My name" {
		t.Fatalf("persisted nickname: %+v, %v", loaded.SessionNames, err)
	}
}

func TestClearRenameRestoresTitle(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{{ID: "s", Title: "Agent title", UpdatedAt: "2026-10-01T10:00:00Z"}}
	m.openSessions()
	m.populateSessions()
	selectListID(m.list, "s")
	m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m.form.Questions[0].Field.Input.SetValue("   ")
	action, _ := m.form.Key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != overlays.ActionSubmit {
		t.Fatal("enter did not submit")
	}
	m.applyRename()
	if _, ok := m.sessionNames[storage.SessionKey("s", m.opts.Descriptor.ID)]; ok {
		t.Fatal("nickname should be cleared")
	}
	item, _ := m.list.Selected()
	if item.Label != "Agent title" {
		t.Fatalf("label after clearing: %q", item.Label)
	}
}

func TestEscapeCancelsRename(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{{ID: "s", Title: "Agent title", UpdatedAt: "2026-10-01T10:00:00Z"}}
	m.openSessions()
	m.populateSessions()
	selectListID(m.list, "s")
	m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.overlay != "sessions" || m.form != nil {
		t.Fatal("esc did not cancel rename")
	}
	if _, ok := m.sessionNames[storage.SessionKey("s", m.opts.Descriptor.ID)]; ok {
		t.Fatal("cancel stored a nickname")
	}
}

func TestCtrlCCancelsRenameAndKeepsPicker(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{{ID: "s", Title: "Agent title", UpdatedAt: "2026-10-01T10:00:00Z"}}
	m.openSessions()
	m.populateSessions()
	selectListID(m.list, "s")
	m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.overlay != "sessions" || m.form != nil || m.list == nil {
		t.Fatal("ctrl+c did not cancel rename and keep the picker")
	}
}

func TestDemoSkipsSessionNamePersistence(t *testing.T) {
	m := testModel(t)
	m.opts.Demo = true
	m.sessionNames[storage.SessionKey("s", m.opts.Descriptor.ID)] = "Nickname"
	_, snapshot := m.Snapshot()
	if len(snapshot.SessionNames) != 0 {
		t.Fatal("demo persisted session names")
	}
}

func TestInteractionPreemptsRenameForm(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{{ID: "s", Title: "Agent title", UpdatedAt: "2026-10-01T10:00:00Z"}}
	m.openSessions()
	m.populateSessions()
	selectListID(m.list, "s")
	m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.form == nil {
		t.Fatal("rename form did not open")
	}
	i := &harness.Interaction{ID: "permission-1", Kind: "permission", SessionID: "s", Reply: make(chan harness.Decision, 1)}
	m.state.Apply(harness.Event{Kind: harness.InteractionRequested, Generation: 1, SessionID: "s", Interaction: i})
	m.syncInteraction()
	if m.form != nil || m.interaction == nil || m.interaction.Kind != "permission" {
		t.Fatal("stale rename form survived the interaction")
	}
}
