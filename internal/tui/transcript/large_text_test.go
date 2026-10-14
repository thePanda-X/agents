package transcript

import (
	"strings"
	"testing"

	"agents/internal/app"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLongUserMessagesFoldWithoutLosingContentOrRowPositions(t *testing.T) {
	for _, compact := range []bool{false, true} {
		entry := &app.Entry{ID: "prompt", Role: "user", Text: strings.Repeat("source line with 界\n", 60), Revision: 1}
		c := &app.Conversation{Entries: []*app.Entry{entry, {Role: "assistant", Text: "following response"}}}
		r := New()
		opts := Options{Width: 60, Compact: compact}
		folded, foldedPositions := r.Render(c, opts)
		if !strings.Contains(ansi.Strip(folded), "collapsed") || strings.Contains(ansi.Strip(folded), "source line") {
			t.Fatal("long user message did not fold")
		}
		if lipgloss.Height(folded) > 12 || !strings.Contains(ansi.Strip(r.Detail(entry, 60, nil)), "source line") {
			t.Fatal("fold overflowed or inspection lost the full text")
		}
		opts.Expanded = map[string]bool{"prompt0": true}
		expanded, expandedPositions := r.Render(c, opts)
		if !strings.Contains(ansi.Strip(expanded), "source line") || expandedPositions[1] <= foldedPositions[1] {
			t.Fatal("expansion reused a collapsed cache block or incorrect row positions")
		}
		opts.Expanded["prompt0"] = false
		again, positions := r.Render(c, opts)
		if again != folded || positions[1] != foldedPositions[1] || entry.Text != strings.Repeat("source line with 界\n", 60) {
			t.Fatal("collapse changed source text or cached row positions")
		}
		for _, line := range strings.Split(expanded, "\n") {
			if ansi.StringWidth(line) > 60 {
				t.Fatal("expanded long prompt overflowed its width")
			}
		}
	}
}

func BenchmarkFoldedLargeUserMessage(b *testing.B) {
	entry := &app.Entry{ID: "prompt", Role: "user", Text: strings.Repeat("large pasted source line\n", 10000), Revision: 1}
	c := &app.Conversation{Entries: []*app.Entry{entry}}
	r := New()
	opts := Options{Width: 100}
	r.Render(c, opts)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		r.Render(c, opts)
	}
}
