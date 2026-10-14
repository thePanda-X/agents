package transcript

import (
	"fmt"
	"strings"
	"testing"

	"agents/internal/app"

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
