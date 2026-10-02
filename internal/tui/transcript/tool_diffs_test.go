package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	"github.com/charmbracelet/x/ansi"
)

func rawToolData(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExpandedToolsShowDiffFallbacks(t *testing.T) {
	patch := "*** Begin Patch\n*** Update File: file.go\n@@\n-old\n+new\n*** End Patch"
	for _, tc := range []struct {
		name string
		tool app.ToolState
		want []string
	}{
		{"patch", app.ToolState{Title: "functions.patch", Input: rawToolData(t, map[string]string{"patchText": patch})}, []string{"Requested patch", "-old", "+new"}},
		{"freeform patch", app.ToolState{Title: "apply_patch", Input: rawToolData(t, patch)}, []string{"Requested patch", "-old", "+new"}},
		{"patch content", app.ToolState{Title: "apply_patch", Input: rawToolData(t, map[string]string{"content": patch})}, []string{"Requested patch", "-old", "+new"}},
		{"write", app.ToolState{Title: "Write file.go", Input: rawToolData(t, map[string]string{"file_path": "file.go", "content": "first\nsecond"})}, []string{"previous contents unavailable", "+first", "+second"}},
		{"write path title", app.ToolState{Title: "file.go", Kind: "edit", Input: rawToolData(t, map[string]string{"filePath": "file.go", "content": "first"})}, []string{"Requested write", "+first"}},
		{"edit", app.ToolState{Title: "Edit", Input: rawToolData(t, map[string]string{"file_path": "file.go", "old_string": "old\n", "new_string": "new\n"})}, []string{"supplied text only", "-old", "+new"}},
		{"reported patch", app.ToolState{Title: "Write", Output: rawToolData(t, map[string]string{"diff": "--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new"})}, []string{"Reported patch", "-old", "+new"}},
		{"reported file diff", app.ToolState{Title: "file.go", Kind: "edit", Output: rawToolData(t, map[string]any{"metadata": map[string]any{"filediff": map[string]string{"file": "file.go", "before": "old\n", "after": "new\n"}}})}, []string{"Reported file diff", "-old", "+new"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := &app.Entry{ID: "tool", Tool: &tc.tool}
			c := &app.Conversation{Entries: []*app.Entry{entry}}
			r := New()
			expanded, _ := r.Render(c, Options{Width: 100, Expanded: map[string]bool{"tool": true}})
			plain := ansi.Strip(expanded)
			for _, want := range tc.want {
				if !strings.Contains(plain, want) {
					t.Fatalf("missing %q:\n%s", want, plain)
				}
			}
			collapsed, _ := r.Render(c, Options{Width: 100})
			if strings.Contains(ansi.Strip(collapsed), tc.want[0]) {
				t.Fatal("diff preview leaked into collapsed row")
			}
			detail := ansi.Strip(r.Detail(entry, 100, nil))
			if !strings.Contains(detail, tc.want[0]) {
				t.Fatal("detail pager lost diff preview")
			}
		})
	}
}

func TestToolDiffFallbacksAreConservativeAndPreferStructuredDiffs(t *testing.T) {
	input := rawToolData(t, map[string]string{"file_path": "file.go", "content": "new text"})
	for _, tool := range []*app.ToolState{
		{Title: "Read", Input: input},
		{Title: "Write", Input: json.RawMessage(`{"content":`)},
		{Title: "Write", Input: rawToolData(t, map[string]string{"content": "no path"})},
		{Title: "Edit", Input: rawToolData(t, map[string]string{"file_path": "file.go", "old_string": "old only"})},
		{Title: "Write", Input: input, Diffs: []harness.Diff{{Path: "file.go", OldText: "old", NewText: "new"}}},
		{Title: "Write", Input: rawToolData(t, map[string]string{"file_path": "file.go", "content": strings.Repeat("x", 128<<10)})},
	} {
		if previews := toolDiffPreviews(tool); len(previews) != 0 {
			t.Fatalf("invented or duplicate diff: %v", previews)
		}
	}
}

func TestCompactGroupsDiffPreviewsUnlessExpandedAndPositionsStayAccurate(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{ID: "read", Tool: &app.ToolState{Title: "Read", Status: "completed"}},
		{ID: "write", Tool: &app.ToolState{Title: "Write", Status: "completed", Input: rawToolData(t, map[string]string{"file_path": "file.go", "content": "one\ntwo"})}},
		{Role: "assistant", Text: "FINAL RESPONSE"},
	}}
	for _, expanded := range []bool{false, true} {
		content, positions := New().Render(c, Options{Width: 60, Compact: true, Expanded: map[string]bool{"write": expanded}})
		lines := strings.Split(ansi.Strip(content), "\n")
		if !strings.Contains(lines[positions[2]], "FINAL RESPONSE") {
			t.Fatal("tool grouping broke response navigation position")
		}
		if expanded {
			if positions[0] == positions[1] || !strings.Contains(lines[positions[1]], "Write") {
				t.Fatal("expanded write was hidden")
			}
		} else if positions[0] != positions[1] || !strings.Contains(lines[positions[1]], "2 successful tools") {
			t.Fatal("completed write preview did not join the tool group")
		}
	}
}
