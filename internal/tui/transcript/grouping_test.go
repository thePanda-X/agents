package transcript

import (
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	"github.com/charmbracelet/x/ansi"
)

func TestCompactComposeGroupsFileToolsAndBrowseRestoresThem(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{ID: "read", Tool: &app.ToolState{Title: "Read file", Status: "completed"}},
		{ID: "patch", Tool: &app.ToolState{Title: "Patch file", Status: "completed", Diffs: []harness.Diff{{Path: "file.go", OldText: "old\n", NewText: "new\n"}}}},
		{ID: "write", Tool: &app.ToolState{Title: "Write file", Status: "completed", Input: rawToolData(t, map[string]string{"file_path": "file.go", "content": "content"})}},
		{ID: "shell", Tool: &app.ToolState{Title: "Run command", Status: "completed"}},
		{ID: "failed", Tool: &app.ToolState{Title: "Failed patch", Status: "failed"}},
		{ID: "pending", Tool: &app.ToolState{Title: "Pending write", Status: "pending"}},
		{Role: "assistant", Text: "FINAL"},
	}}
	for _, focused := range []bool{false, true} {
		content, positions := New().Render(c, Options{Width: 60, Compact: true, Focused: focused})
		lines := strings.Split(ansi.Strip(content), "\n")
		if !focused {
			if !strings.Contains(lines[positions[0]], "4 successful tools") {
				t.Fatal("file tools did not join the mixed successful group")
			}
			for _, position := range positions[:4] {
				if position != positions[0] {
					t.Fatal("grouped entries have inconsistent positions")
				}
			}
		} else {
			for index, title := range []string{"Read file", "Patch file", "Write file", "Run command"} {
				if !strings.Contains(lines[positions[index]], title) || index > 0 && positions[index] <= positions[index-1] {
					t.Fatal("browse did not restore individual file tools")
				}
			}
		}
		for index, title := range map[int]string{4: "Failed patch", 5: "Pending write", 6: "FINAL"} {
			if !strings.Contains(lines[positions[index]], title) {
				t.Fatal("grouping hid a failed/active tool or broke response positions")
			}
		}
	}
}

func TestCompactComposeGroupsThinkingAndToolsButBrowseRestoresEntries(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{Role: "user", Text: "PROMPT"},
		{ID: "thought", Role: "thought", Text: "Reasoning text"},
		{ID: "read", Tool: &app.ToolState{Title: "Read file", Status: "completed"}},
		{ID: "more", Role: "thought", Text: "More reasoning"},
		{ID: "patch", Tool: &app.ToolState{Title: "Patch file", Status: "completed"}},
		{Role: "assistant", Text: "FINAL"},
	}}
	for _, focused := range []bool{false, true} {
		content, positions := New().Render(c, Options{Width: 80, Compact: true, Focused: focused, Thoughts: true})
		lines := strings.Split(ansi.Strip(content), "\n")
		if focused {
			for index, marker := range map[int]string{1: "Thinking", 2: "Read file", 3: "Thinking", 4: "Patch file"} {
				if !strings.Contains(lines[positions[index]], marker) || index > 1 && positions[index] <= positions[index-1] {
					t.Fatal("browse did not restore thinking/tool entries")
				}
			}
		} else {
			if !strings.Contains(lines[positions[1]], "2 successful tools · 2 thinking blocks") {
				t.Fatal("mixed group has incorrect counts")
			}
			for _, position := range positions[1:5] {
				if position != positions[1] {
					t.Fatal("mixed group navigation positions differ")
				}
			}
			if strings.Contains(ansi.Strip(content), "Reasoning text") {
				t.Fatal("global thought visibility bypassed compose grouping")
			}
		}
		if !strings.Contains(lines[positions[5]], "FINAL") || strings.TrimSpace(lines[positions[1]-1]) != "" || strings.TrimSpace(lines[positions[5]-1]) != "" {
			t.Fatal("grouping lost user/response boundaries or response positions")
		}
	}
	content, positions := New().Render(c, Options{Width: 80, Compact: true, Expanded: map[string]bool{"thought1": true}})
	if positions[1] == positions[2] || !strings.Contains(ansi.Strip(content), "Reasoning text") {
		t.Fatal("explicitly expanded thought was hidden")
	}
}
