package tui

import (
	"strings"
	"testing"
	"time"

	"agents/internal/harness"
	"agents/internal/storage"

	"github.com/charmbracelet/x/ansi"
)

func TestSessionsGroupedByLocalDayNewestFirst(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("test-local", -7*60*60)
	t.Cleanup(func() { time.Local = previousLocal })
	m := testModel(t)
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{
		{ID: "older", Title: "Match older", UpdatedAt: "2026-10-12T12:00:00-07:00"},
		{ID: "missing", Title: "Missing date"},
		{ID: "offset", Title: "Match offset", UpdatedAt: "2026-10-14T00:30:00+02:00"},
		{ID: "latest", Title: "Match latest", UpdatedAt: "2026-10-13T16:00:00-07:00"},
		{ID: "invalid", Title: "Bad date", UpdatedAt: "not-a-date"},
		{ID: "fraction", Title: "Match fraction", UpdatedAt: "2026-10-13T23:00:00.1Z"},
		{ID: "previous-year", Title: "Previous year", UpdatedAt: "2025-10-13T16:00:00-07:00"},
	}
	m.openSessions()
	m.populateSessions()
	for i, want := range []string{"fraction", "latest", "offset", "older", "previous-year", "missing", "invalid"} {
		if got := m.list.Items[i].ID; got != want {
			t.Fatalf("session %d: got %s, want %s", i, got, want)
		}
	}
	for i, want := range []string{"October 13, 2026", "October 13, 2026", "October 13, 2026", "October 12, 2026", "October 13, 2025", "Unknown date", "Unknown date"} {
		if got := m.list.Items[i].Group; got != want {
			t.Fatalf("session %d grouped under %q, want %q", i, got, want)
		}
	}
	if m.list.Items[0].Detail != "16:00" {
		t.Fatal("session time is not local")
	}
	view := ansi.Strip(m.list.View(70, 22))
	last := -1
	for _, date := range []string{"─ October 13, 2026 ─", "─ October 12, 2026 ─", "─ October 13, 2025 ─", "─ Unknown date ─"} {
		index := strings.Index(view, date)
		if index <= last || strings.Count(view, date) != 1 {
			t.Fatalf("missing, repeated, or unordered day heading %q:\n%s", date, view)
		}
		last = index
	}
	// Filtering must not fuzzy-rank an older session above a newer one.
	m.list.Search.SetValue("Match older")
	if item, ok := m.list.Selected(); !ok || item.ID != "older" {
		t.Fatal("filter did not find the older session")
	}
	m.list.Search.SetValue("Match")
	for i, item := range m.list.Filtered() {
		if item.ID != m.list.Items[i].ID {
			t.Fatal("filtering changed chronological order")
		}
	}
}

func TestLiveSessionDateUpdateRegroupsPickerAndPersists(t *testing.T) {
	m := testModel(t)
	m.opts.Store = &storage.Store{Directory: t.TempDir()}
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{
		{ID: "s", Title: "Current", Workspace: "/workspace", UpdatedAt: "2026-10-01T10:00:00Z"},
		{ID: "other", Title: "Other", Workspace: "/workspace", UpdatedAt: "2026-10-12T10:00:00Z"},
	}
	m.openSessions()
	m.populateSessions()
	selectListID(m.list, "s")
	const updated = "2026-10-13T23:45:00.123456789-07:00"
	m.Update(eventMsg(harness.Event{Kind: harness.Metadata, Generation: 1, SessionID: "s", Metadata: harness.SessionInfoPatch{UpdatedAt: new(updated)}}))
	if m.list.Items[0].ID != "s" || m.list.Items[0].Group != sessionDay(updated) {
		t.Fatal("live date update did not move the session to the latest day")
	}
	if selected, ok := m.list.Selected(); !ok || selected.ID != "s" {
		t.Fatal("regrouping lost the selected session")
	}
	settings, snapshot := m.Snapshot()
	if err := m.opts.Store.Save(settings, snapshot); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := m.opts.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workspaces[storage.Key("/workspace", m.opts.Descriptor.ID)].Session.UpdatedAt != updated {
		t.Fatal("persisted session lost the reported offset or precision")
	}
}
