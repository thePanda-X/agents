package tui

import (
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"
)

func TestSubagentEditsAppearInChangesAndSearch(t *testing.T) {
	m := testModel(t)
	edit := &app.Entry{Kind: "tool", Tool: &app.ToolState{Title: "Edit nested.go", Status: "failed", Diffs: []harness.Diff{{Path: "nested.go", OldText: "a", NewText: "b"}}}}
	note := &app.Entry{Kind: "message", Role: "assistant", Text: "subagent needle"}
	launch := &app.Entry{Kind: "tool", Tool: &app.ToolState{Title: "Agent", Status: "completed"}, Children: []*app.Entry{edit, note}}
	m.state.Active().Entries = []*app.Entry{{Role: "user", Text: "go"}, launch}

	m.openChanges()
	if len(m.list.Items) != 1 || m.list.Items[0].ID != "nested.go" || !strings.Contains(m.list.Items[0].Detail, "failed") {
		t.Fatalf("subagent edit missing or attributed to its launch: %+v", m.list.Items)
	}
	m.choose(m.list.Items[0])
	if m.selected != 1 || !strings.Contains(m.info.GetContent(), "Reported tool status: failed") {
		t.Fatalf("inspecting a subagent edit should select its launch: selected=%d", m.selected)
	}
	if text := m.entryText(launch); !strings.Contains(text, "subagent needle") || !strings.Contains(text, "Edit nested.go") {
		t.Fatalf("subagent work is not searchable through its launch: %q", text)
	}
}
