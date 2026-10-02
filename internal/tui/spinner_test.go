package tui

import (
	"testing"
	"time"

	"agents/internal/harness"
)

func TestTicksAnimatePendingToolsWithoutMovingReadingPosition(t *testing.T) {
	m := testModel(t)
	status := "pending"
	title := "Patch file"
	m.Update(eventMsg(harness.Event{Kind: harness.Tool, Generation: 1, SessionID: "s", Tool: harness.ToolPatch{ID: "tool", Title: &title, Status: &status}}))
	m.Update(tickMsg(time.UnixMilli(0)))
	first := m.viewport.GetContent()
	position := m.positions[0]
	m.Update(tickMsg(time.UnixMilli(120)))
	if first == m.viewport.GetContent() || m.positions[0] != position {
		t.Fatal("pending tool did not animate or moved its reading position")
	}
	status = "completed"
	m.Update(eventMsg(harness.Event{Kind: harness.Tool, Generation: 1, SessionID: "s", Tool: harness.ToolPatch{ID: "tool", Status: &status}}))
	m.Update(tickMsg(time.UnixMilli(240)))
	finished := m.viewport.GetContent()
	m.Update(tickMsg(time.UnixMilli(360)))
	if finished != m.viewport.GetContent() {
		t.Fatal("finished tool continued animating")
	}
}
