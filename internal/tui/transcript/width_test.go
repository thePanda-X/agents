package transcript

import (
	"fmt"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	"github.com/charmbracelet/x/ansi"
)

func TestTranscriptTextUsesAvailableWidth(t *testing.T) {
	for _, role := range []string{"assistant", "user", "thought"} {
		for _, compact := range []bool{false, true} {
			for _, focused := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/compact=%v/focused=%v", role, compact, focused), func(t *testing.T) {
					c := &app.Conversation{Entries: []*app.Entry{
						{Role: role, Text: strings.Repeat("words 界 ", 100) + "finalword"},
						{Role: "assistant", Text: "NEXT ENTRY"},
					}}
					r := New()
					// Reuse the renderer across growth and shrinkage to exercise cached widths.
					for _, width := range []int{32, 60, 120, 160, 240, 40, 160} {
						content, positions := r.Render(c, Options{Width: width, Compact: compact, Focused: focused, Thoughts: true})
						plain := ansi.Strip(content)
						if strings.Count(plain, "words") != 100 || !strings.Contains(plain, "finalword") {
							t.Fatal("wrapping lost transcript text")
						}
						lines := strings.Split(plain, "\n")
						widest := 0
						for _, line := range lines {
							if got := ansi.StringWidth(line); got > width {
								t.Fatalf("row exceeds width %d: %q", width, line)
							}
							widest = max(widest, ansi.StringWidth(strings.TrimRight(line, " ")))
						}
						if widest < width-12 {
							t.Fatalf("content wraps too early: widest row is %d cells, available width is %d", widest, width)
						}
						if positions[1] >= len(lines) || !strings.Contains(lines[positions[1]], "NEXT ENTRY") {
							t.Fatal("reflow broke the following entry's row position")
						}
					}
				})
			}
		}
	}
}

func TestNestedMarkdownUsesAvailableWidth(t *testing.T) {
	prose := strings.Repeat("Readable **prose** 界 words. ", 40)
	for _, source := range []string{"- " + prose, "> " + prose, "> - " + prose} {
		for _, width := range []int{32, 60, 160, 240} {
			rendered, err := newMarkdownRenderer(width).Render(source)
			if err != nil {
				t.Fatal(err)
			}
			widest := 0
			for _, line := range strings.Split(ansi.Strip(rendered), "\n") {
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("nested Markdown exceeds width %d: %q", width, line)
				}
				widest = max(widest, ansi.StringWidth(strings.TrimRight(line, " ")))
			}
			if widest < width-12 {
				t.Fatalf("nested prose wraps too early: widest row is %d cells, available width is %d", widest, width)
			}
			if strings.Count(ansi.Strip(rendered), "Readable") != 40 {
				t.Fatal("nested Markdown lost text")
			}
		}
	}
}

// TestUnbreakableTokensNeverExceedWidth guards the invariant that rendered
// transcript rows never exceed the viewport width, even when content contains
// tokens that cannot wrap at word boundaries (long URLs, code lines, base64,
// diff lines, and so on). Lines wider than the budget must be hard-broken,
// never left to overflow into horizontal panning.
func TestUnbreakableTokensNeverExceedWidth(t *testing.T) {
	token := strings.Repeat("abcdefgh", 25)
	longURL := "https://example.com/" + strings.Repeat("seg", 30)
	cases := []struct {
		name   string
		entry  *app.Entry
		needle string
	}{
		{"assistant link", &app.Entry{Role: "assistant", Text: "See " + longURL + " for details."}, longURL},
		{"assistant code", &app.Entry{Role: "assistant", Text: "```go\n" + strings.Repeat("x", 200) + "\n```"}, strings.Repeat("x", 200)},
		{"user token", &app.Entry{Role: "user", Text: "paste " + token}, token},
		{"thought token", &app.Entry{Role: "thought", Text: token + " reasoning"}, token},
		{"tool input", &app.Entry{ID: "tool", Tool: &app.ToolState{Title: "Write file.go", Status: "completed", Input: rawToolData(t, map[string]string{"file_path": "f.go", "content": token})}}, token},
		{"tool diff", &app.Entry{ID: "tool", Tool: &app.ToolState{Title: "Edit file.go", Status: "completed", Diffs: []harness.Diff{{Path: "f.go", OldText: "old", NewText: "newline\n" + token}}}}, token},
	}
	for _, tc := range cases {
		for _, width := range []int{28, 60} {
			t.Run(tc.name+"/w="+fmt.Sprint(width), func(t *testing.T) {
				c := &app.Conversation{Entries: []*app.Entry{tc.entry}}
				content, positions := New().Render(c, Options{Width: width, Thoughts: true, Expanded: map[string]bool{"tool": true}})
				lines := strings.Split(content, "\n")
				for i, line := range lines {
					if got := ansi.StringWidth(ansi.Strip(line)); got > width {
						t.Fatalf("line %d exceeds width %d: %q (%d cells)", i, width, line, got)
					}
				}
				for i, pos := range positions {
					if pos >= len(lines) {
						t.Fatalf("entry %d position %d beyond rendered rows %d", i, pos, len(lines))
					}
				}
				// Hard breaks insert only newlines and trailing spaces; the
				// multiline left rail repeats the │ border per row. Stripping
				// those must restore the original token sequence, proving no
				// content was dropped.
				compact := strings.Map(func(r rune) rune {
					if r == ' ' || r == '\n' || r == '│' {
						return -1
					}
					return r
				}, ansi.Strip(content))
				if !strings.Contains(compact, tc.needle) {
					t.Fatal("hard wrapping lost token content")
				}
			})
		}
	}
}
