package transcript

import (
	"fmt"
	"strings"
	"testing"

	"agents/internal/app"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func subagentLaunch(status string, steps int) *app.Entry {
	launch := &app.Entry{ID: "task", Kind: "tool", Revision: 1, Tool: &app.ToolState{ID: "task", Title: "Explore the parser", Status: status, Input: []byte(`{"prompt":"find the parser"}`)}}
	launch.Children = append(launch.Children, &app.Entry{Kind: "message", Role: "user", Text: "find the parser"})
	for i := range steps {
		launch.Children = append(launch.Children, &app.Entry{ID: fmt.Sprint("read-", i), Kind: "tool", Tool: &app.ToolState{Title: fmt.Sprintf("Read file%02d.go", i), Status: "completed"}})
	}
	launch.Children = append(launch.Children, &app.Entry{Kind: "message", Role: "assistant", Text: "The parser lives in parse.go.\nIt is hand-written."})
	return launch
}

func TestCollapsedRunningSubagentShowsLatestStep(t *testing.T) {
	launch := subagentLaunch("in_progress", 3)
	c := &app.Conversation{Entries: []*app.Entry{launch}}
	out := ansi.Strip(must(New().Render(c, Options{Width: 60, SpinnerFrame: -1})))
	if !strings.Contains(out, "3 steps") || !strings.Contains(out, "⎿ It is hand-written.") {
		t.Fatalf("running subagent did not show its progress:\n%s", out)
	}
	if strings.Contains(out, "file00") {
		t.Fatalf("collapsed subagent expanded its steps:\n%s", out)
	}
	launch.Tool.Status = "completed"
	out = ansi.Strip(must(New().Render(c, Options{Width: 60})))
	if strings.Contains(out, "⎿") || !strings.Contains(out, "3 steps") {
		t.Fatalf("finished subagent should collapse to one line:\n%s", out)
	}
}

func TestExpandedSubagentShowsRecentStepsWithinWidth(t *testing.T) {
	launch := subagentLaunch("in_progress", subagentTail+4)
	launch.Children = append(launch.Children, &app.Entry{Kind: "message", Role: "assistant", Text: strings.Repeat("unbreakable", 20)})
	after := &app.Entry{Kind: "message", Role: "assistant", Text: "Main reply", Revision: 1}
	c := &app.Conversation{Entries: []*app.Entry{launch, after}}
	width := 40
	rendered, positions := New().Render(c, Options{Width: width, SpinnerFrame: -1, Expanded: map[string]bool{"task": true}})
	out := ansi.Strip(rendered)
	if !strings.Contains(out, "earlier steps") || strings.Contains(out, "file00") || !strings.Contains(out, fmt.Sprintf("file%02d", subagentTail+3)) {
		t.Fatalf("expanded subagent should show the latest steps:\n%s", out)
	}
	if strings.Count(out, "find the parser") != 1 {
		t.Fatalf("subagent prompt repeated beyond the launch input:\n%s", out)
	}
	lines := strings.Split(rendered, "\n")
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line exceeds width %d (%d): %q", width, w, ansi.Strip(line))
		}
	}
	if !strings.Contains(ansi.Strip(lines[positions[1]]), "Main reply") {
		t.Fatalf("position of entry after subagent is wrong: row %d = %q", positions[1], ansi.Strip(lines[positions[1]]))
	}
}

func TestSubagentDetailShowsEverythingIncludingNestedAgents(t *testing.T) {
	launch := subagentLaunch("completed", subagentTail+4)
	nested := &app.Entry{ID: "inner", Kind: "tool", Tool: &app.ToolState{Title: "Inner research", Status: "completed"}}
	nested.Children = []*app.Entry{{Kind: "tool", Tool: &app.ToolState{Title: "Grep TODO", Status: "completed"}}}
	launch.Children = append(launch.Children, nested)
	out := ansi.Strip(New().Detail(launch, 60, nil))
	for _, want := range []string{"file00", "file11", "It is hand-written.", "Inner research", "Grep TODO"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "earlier steps") {
		t.Fatalf("detail should not truncate steps:\n%s", out)
	}
}

func TestCompactSummaryCountsSubagents(t *testing.T) {
	launch := subagentLaunch("completed", 1)
	read := &app.Entry{ID: "r", Kind: "tool", Tool: &app.ToolState{Kind: "read", Status: "completed"}}
	if got := activitySummary([]*app.Entry{launch, read}); got != "1 subagent · read 1 file" {
		t.Fatalf("summary = %q", got)
	}
}

func must(out string, _ []int) string { return out }
