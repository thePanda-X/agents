package transcript

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agents/internal/app"
	"agents/internal/harness"

	"github.com/charmbracelet/x/ansi"
)

func renderPlain(c *app.Conversation, options Options) (string, []int) {
	content, positions := New().Render(c, options)
	return ansi.Strip(content), positions
}

func TestExpandedToolBodyHangsOffRailAndNextActionStandsApart(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{ID: "r", Tool: &app.ToolState{Title: "Read a.go", Status: "completed", Text: "package a\n\nimport ("}},
		{ID: "s", Tool: &app.ToolState{Title: "Search", Status: "completed"}},
	}}
	plain, positions := renderPlain(c, Options{Width: 50, Expanded: map[string]bool{"r": true}})
	lines := strings.Split(plain, "\n")
	for _, want := range []string{"package a", "import ("} {
		found := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "│ "+want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("body line %q is not on the rail:\n%s", want, plain)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[positions[1]]), "▸ Search") {
		t.Fatalf("next action row %d = %q, want its heading:\n%s", positions[1], lines[positions[1]], plain)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 50 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}

func TestToolDetailOmitsRail(t *testing.T) {
	entry := &app.Entry{ID: "r", Tool: &app.ToolState{Title: "Read", Status: "completed", Text: "body text"}}
	plain := ansi.Strip(New().Detail(entry, 40, nil))
	if strings.Contains(plain, "│") || !strings.Contains(plain, "body text") {
		t.Fatalf("detail = %q", plain)
	}
}

func TestExpandedToolLocationsAreWorkspaceRelativeAndNotRepeatedByDiffs(t *testing.T) {
	c := &app.Conversation{Info: harness.SessionInfo{Workspace: "/repo"}, Entries: []*app.Entry{
		{ID: "e", Tool: &app.ToolState{Title: "Edit", Status: "completed",
			Locations: []string{"/repo/a.go", "/repo/internal/b.go:12", "/elsewhere/c.go"},
			Diffs:     []harness.Diff{{Path: "/repo/a.go", OldText: "x\n", NewText: "y\n"}}}},
	}}
	plain, _ := renderPlain(c, Options{Width: 60, Expanded: map[string]bool{"e": true}})
	if strings.Count(plain, "a.go") != 2 { // only the ---/+++ diff headers
		t.Fatalf("diffed location repeated:\n%s", plain)
	}
	if !strings.Contains(plain, "│ internal/b.go:12") || !strings.Contains(plain, "/elsewhere/c.go") {
		t.Fatalf("locations not shortened as expected:\n%s", plain)
	}
}

func TestToolResultIsLabeledOnlyBesidePayloadSections(t *testing.T) {
	labeled := &app.Entry{ID: "x", Tool: &app.ToolState{Title: "Run", Status: "failed", Text: "boom", Input: json.RawMessage(`{"command":"go"}`)}}
	plain, _ := renderPlain(&app.Conversation{Entries: []*app.Entry{labeled}}, Options{Width: 60, Expanded: map[string]bool{"x": true}})
	if !strings.Contains(plain, "│ Input") || !strings.Contains(plain, "│ Result") {
		t.Fatalf("missing labels:\n%s", plain)
	}
	bare := &app.Entry{ID: "r", Tool: &app.ToolState{Title: "Read", Status: "completed", Text: "contents"}}
	plain, _ = renderPlain(&app.Conversation{Entries: []*app.Entry{bare}}, Options{Width: 60, Expanded: map[string]bool{"r": true}})
	if strings.Contains(plain, "Result") {
		t.Fatalf("lone result should not be captioned:\n%s", plain)
	}
}

func TestExpandedToolWithoutDetailsSaysSo(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{{ID: "t", Tool: &app.ToolState{Title: "Ping", Status: "completed"}}}}
	plain, _ := renderPlain(c, Options{Width: 60, Expanded: map[string]bool{"t": true}})
	if !strings.Contains(plain, "No details reported by the agent.") {
		t.Fatalf("empty expansion gave no feedback:\n%s", plain)
	}
}

func TestCollapsedFailedToolShowsReason(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{ID: "x", Tool: &app.ToolState{Title: "Run tests", Status: "failed", Text: "\n  exit status 2: build failed\nmore"}},
		{ID: "n", Tool: &app.ToolState{Title: "Next", Status: "completed"}},
	}}
	plain, positions := renderPlain(c, Options{Width: 60})
	lines := strings.Split(plain, "\n")
	if got := strings.TrimSpace(lines[1]); got != "⎿ exit status 2: build failed" {
		t.Fatalf("failure hint = %q:\n%s", got, plain)
	}
	if positions[1] != 2 {
		t.Fatalf("next entry position = %d, want 2", positions[1])
	}
}

func TestRunningToolShowsElapsedAfterTwoSeconds(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{{time.Second, ""}, {7 * time.Second, "running · 7s"}, {75*time.Second + 400*time.Millisecond, "running · 1m 15s"}} {
		entry := &app.Entry{ID: "t", At: now.Add(-tc.age), Tool: &app.ToolState{Title: "Build", Status: "in_progress"}}
		plain, _ := renderPlain(&app.Conversation{Entries: []*app.Entry{entry}}, Options{Width: 60, ReducedMotion: true, Now: now})
		if tc.want == "" && strings.Contains(plain, "·") || tc.want != "" && !strings.Contains(plain, tc.want) {
			t.Fatalf("age %v: heading = %q, want %q", tc.age, plain, tc.want)
		}
	}
	pending := &app.Entry{ID: "p", At: now.Add(-time.Minute), Tool: &app.ToolState{Title: "Queue", Status: "pending"}}
	plain, _ := renderPlain(&app.Conversation{Entries: []*app.Entry{pending}}, Options{Width: 60, ReducedMotion: true, Now: now})
	if strings.Contains(plain, "1m") {
		t.Fatalf("pending tool should not count time: %q", plain)
	}
}
