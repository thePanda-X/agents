package transcript

import (
	"fmt"
	"strings"
	"testing"

	"agents/internal/app"

	"github.com/charmbracelet/x/ansi"
)

func TestLinkAtRenderedCells(t *testing.T) {
	target := "https://example.com/full?one=1&two=2#section"
	linked := "  Before " + ansi.SetHyperlink(target, "id=one") + "\x1b[1m界e\u0301🔗\x1b[m" + ansi.ResetHyperlink() + " after"
	for _, test := range []struct {
		name, line string
		column     int
		want       string
	}{
		{"gutter", linked, 1, ""},
		{"before", linked, 8, ""},
		{"wide first cell", linked, 9, target},
		{"wide second cell", linked, 10, target},
		{"combining character", linked, 11, target},
		{"emoji", linked, 12, target},
		{"emoji second cell", linked, 13, target},
		{"after reset", linked, 14, ""},
		{"past row", linked, 100, ""},
		{"ST terminator", "  \x1b]8;;" + target + "\x1b\\label\x1b]8;;\x1b\\.", 3, target},
		{"C1 terminator", "  \x9d8;;" + target + "\x9clabel\x9d8;;\x9c.", 3, target},
		{"literal URL", "  See https://example.com/page.", 12, "https://example.com/page"},
		{"literal URL punctuation", "  See https://example.com/page.", len("  See https://example.com/page"), ""},
		{"balanced parentheses", "  (https://example.com/a(b)).", 12, "https://example.com/a(b)"},
		{"square brackets", "  [https://example.com/page],", 12, "https://example.com/page"},
		{"HTTP localhost", "  http://localhost:3000", 4, "http://localhost:3000"},
		{"unsafe OSC target is not replaced by label", "  " + ansi.SetHyperlink("file:///secret") + "https://example.com" + ansi.ResetHyperlink(), 5, "file:///secret"},
		{"multiple targets", "  " + ansi.SetHyperlink(target) + "one" + ansi.ResetHyperlink() + " " + ansi.SetHyperlink("https://second.example") + "two" + ansi.ResetHyperlink(), 6, "https://second.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := LinkAt(test.line, test.column); got != test.want {
				t.Fatalf("LinkAt(%q, %d) = %q, want %q", test.line, test.column, got, test.want)
			}
		})
	}
}

func TestMarkdownLinksKeepTargetsThroughRendering(t *testing.T) {
	target := "https://example.com/" + strings.Repeat("long-path/", 8) + "?one=1&two=2#section"
	for _, compact := range []bool{false, true} {
		for _, width := range []int{28, 76} {
			for _, test := range []struct{ name, source, label string }{
				{"inline", "Before [label](" + target + ") after.", "label"},
				{"reference", "Before [label][ref] after.\n\n[ref]: " + target, "label"},
				{"autolink", "Before <" + target + "> after.", "https://example.com/"},
				{"bare", "Before " + target + " after.", "https://example.com/"},
				{"table", "| Name | Value |\n| --- | --- |\n| [label](" + target + ") | text |", "label"},
				{"styled label", "Before [**label**](" + target + ") after.", "label"},
			} {
				t.Run(fmt.Sprintf("%s/compact=%v/width=%d", test.name, compact, width), func(t *testing.T) {
					c := &app.Conversation{Entries: []*app.Entry{{Role: "assistant", Text: test.source}}}
					r := New()
					content, _ := r.Render(c, Options{Width: width, Compact: compact, Focused: true, Search: "label"})
					found, rows := false, 0
					for _, line := range strings.Split(content, "\n") {
						plain := ansi.Strip(line)
						// Table footer descriptions are plain labels, not links;
						// the table cell and footer URL carry the hyperlink metadata.
						if index := strings.Index(plain, test.label); index >= 0 && !strings.Contains(plain, "[1]:") {
							found = true
							if got := LinkAt(line, ansi.StringWidth(plain[:index])); got != target {
								t.Fatalf("label target = %q, want %q: %q", got, target, line)
							}
						}
						linked := false
						for column := 2; column < ansi.StringWidth(line); column++ {
							if got := LinkAt(line, column); got != "" {
								linked = true
								if got != target {
									t.Fatalf("wrapped target = %q, want full URL %q", got, target)
								}
							}
						}
						if linked {
							rows++
						}
						if index := strings.Index(plain, "after."); index >= 0 && LinkAt(line, ansi.StringWidth(plain[:index])) != "" {
							t.Fatal("link leaked into following prose")
						}
					}
					if !found || rows < 2 && test.name != "table" {
						t.Fatalf("fixture did not retain label or wrap the link: %q", ansi.Strip(content))
					}
				})
			}
		}
	}
}
