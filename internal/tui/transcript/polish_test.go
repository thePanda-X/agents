package transcript

import (
	"fmt"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

func TestDiffFileHeadersAreNotColoredAsChangesAndHaveNoTrailingBlank(t *testing.T) {
	diff := RenderDiff(harness.Diff{Path: "x.go", OldText: "a\nb\n", NewText: "a\nB\n"})
	lines := strings.Split(diff, "\n")
	added := theme.Success.Render("+")
	added = added[:strings.Index(added, "+")]
	for _, line := range lines {
		plain := ansi.Strip(line)
		if strings.HasPrefix(plain, "+++") && strings.HasPrefix(line, added) {
			t.Fatalf("file header styled as an addition: %q", line)
		}
	}
	if strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		t.Fatal("diff ends with a blank row")
	}
}

func TestToolHeadingShowsMeaningfulStatus(t *testing.T) {
	for _, tc := range []struct {
		tool       app.ToolState
		want, deny string
	}{
		{app.ToolState{Title: "Edit", Status: "completed", Diffs: []harness.Diff{{Path: "x", OldText: "a\nb\nc\n", NewText: "a\nB\nc\nd\n"}}}, "+2 −1", "completed"},
		{app.ToolState{Title: "Read", Status: "completed"}, "Read", "completed"},
		{app.ToolState{Title: "Test", Status: "in_progress"}, "running", "in_progress"},
		{app.ToolState{Title: "Queue", Status: "pending"}, "pending", ""},
		{app.ToolState{Title: "Fail", Status: "failed"}, "failed", ""},
	} {
		c := &app.Conversation{Entries: []*app.Entry{{ID: "t", Tool: &tc.tool}}}
		content, _ := New().Render(c, Options{Width: 60, ReducedMotion: true})
		plain := ansi.Strip(content)
		if !strings.Contains(plain, tc.want) || tc.deny != "" && strings.Contains(plain, tc.deny) {
			t.Fatalf("%s heading = %q, want %q without %q", tc.tool.Title, plain, tc.want, tc.deny)
		}
	}
}

func TestLongToolOutputIsCappedInTranscriptButCompleteInDetail(t *testing.T) {
	var output strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&output, "line %d\n", i)
	}
	entry := &app.Entry{ID: "t", Tool: &app.ToolState{Title: "Run", Status: "completed", Text: output.String()}}
	c := &app.Conversation{Entries: []*app.Entry{entry}}
	content, _ := New().Render(c, Options{Width: 60, Expanded: map[string]bool{"t": true}})
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "line 30") || strings.Contains(plain, "line 31\n") || !strings.Contains(plain, "… 70 more lines") {
		t.Fatalf("output was not capped:\n%s", plain)
	}
	if detail := ansi.Strip(New().Detail(entry, 60, nil)); !strings.Contains(detail, "line 100") || strings.Contains(detail, "more lines") {
		t.Fatal("detail pager lost the complete output")
	}
	searched, _ := New().Render(c, Options{Width: 60, Expanded: map[string]bool{"t": true}, Search: "line 100"})
	if !strings.Contains(ansi.Strip(searched), "line 100") {
		t.Fatal("search cannot reveal output beyond the cap")
	}
}

func TestComfortableLayoutKeepsActivityRunsDense(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{Role: "user", Text: "PROMPT"},
		{ID: "a", Tool: &app.ToolState{Title: "First", Status: "completed"}},
		{ID: "th", Role: "thought", Text: "hmm"},
		{ID: "b", Tool: &app.ToolState{Title: "Second", Status: "failed"}},
		{Role: "assistant", Text: "ANSWER"},
	}}
	for _, focused := range []bool{false, true} {
		content, positions := New().Render(c, Options{Width: 60, Focused: focused, Selected: 2})
		lines := strings.Split(ansi.Strip(content), "\n")
		// A comfortable user message starts with its padding row.
		if !strings.Contains(strings.Join(lines[positions[0]:positions[1]], "\n"), "PROMPT") {
			t.Fatal("prompt is not at its position")
		}
		for index, want := range map[int]string{1: "First", 2: "Thinking", 3: "Second", 4: "ANSWER"} {
			if !strings.Contains(lines[positions[index]], want) {
				t.Fatalf("position %d is %q, want %q", index, lines[positions[index]], want)
			}
		}
		if positions[2] != positions[1]+1 || positions[3] != positions[2]+1 {
			t.Fatalf("activity run is not dense: %v", positions)
		}
		if strings.TrimSpace(lines[positions[1]-1]) != "" || strings.TrimSpace(lines[positions[4]-1]) != "" {
			t.Fatal("prompt and answer lost their separating blank rows")
		}
	}
}

func TestCompactGroupSummarizesActivityByKind(t *testing.T) {
	tool := func(kind string) *app.Entry {
		return &app.Entry{ID: kind + fmt.Sprint(len(kind)), Tool: &app.ToolState{Title: kind, Kind: kind, Status: "completed"}}
	}
	entries := []*app.Entry{tool("read"), tool("read"), tool("edit"), tool("execute"), tool("search"), tool("think"), {Role: "thought", Text: "x"}, tool("mystery")}
	if got := activitySummary(entries); got != "Read 2 files · 1 search · 1 edit · ran 1 command · 1 tool · 2 thoughts" {
		t.Fatalf("summary = %q", got)
	}
}

func TestInlineCodeHasNoPaddingSpaces(t *testing.T) {
	rendered, err := newMarkdownRenderer(60).Render("call `parse()` now.")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(ansi.Strip(rendered)); got != "call parse() now." {
		t.Fatalf("inline code spacing: %q", got)
	}
}

func TestSmallTablesUseTheirNaturalWidth(t *testing.T) {
	rendered, err := newMarkdownRenderer(100).Render("| a | b |\n|---|---|\n| 1 | 2 |")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(ansi.Strip(rendered), "\n") {
		if w := ansi.StringWidth(strings.TrimRight(line, " ")); w > 20 {
			t.Fatalf("tiny table spans %d columns: %q", w, line)
		}
	}
	assertTableFits(t, rendered, 100)
}
