package tui

import (
	"strings"
	"testing"
	"time"

	"agents/internal/harness"

	"github.com/charmbracelet/x/ansi"
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

func TestReducedMotionStillRefreshesRunningToolElapsedEachSecond(t *testing.T) {
	m := testModel(t)
	m.opts.Settings.ReducedMotion = true
	status, title := "in_progress", "Build"
	m.Update(eventMsg(harness.Event{Kind: harness.Tool, Generation: 1, SessionID: "s", Tool: harness.ToolPatch{ID: "tool", Title: &title, Status: &status}}))
	started := m.state.Active().Entries[0].At
	m.Update(tickMsg(started.Add(500 * time.Millisecond)))
	first := m.viewport.GetContent()
	m.Update(tickMsg(started.Add(3 * time.Second)))
	if first == m.viewport.GetContent() || !strings.Contains(ansi.Strip(m.viewport.GetContent()), "running · 3s") {
		t.Fatalf("elapsed label did not refresh:\n%s", ansi.Strip(m.viewport.GetContent()))
	}
}
