package transcript

import (
	"strings"
	"testing"

	"agents/internal/app"

	"github.com/charmbracelet/x/ansi"
)

func TestMixedMarkdownCapsProseWithoutNarrowingCode(t *testing.T) {
	prose := strings.Repeat("Readable prose with a [reference][ref]. ", 12)
	code := strings.Repeat("code_", 24)
	source := prose + "\n\n```text\n" + code + "\n```\n\n" + prose + "\n\n[ref]: https://example.com"
	rendered, err := newMarkdownRenderer(150).Render(source)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, code) || !strings.Contains(plain, "example.com") {
		t.Fatal("mixed rendering narrowed code or lost reference-link resolution")
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "Readable prose") && ansi.StringWidth(line) > 96 {
			t.Fatalf("prose stayed too wide: %d", ansi.StringWidth(line))
		}
	}
}

func TestReducedMotionKeepsRunningToolStatusWithoutAnimation(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{{ID: "tool", Tool: &app.ToolState{Title: "Run tests", Status: "in_progress"}}}}
	r := New()
	first, _ := r.Render(c, Options{Width: 80, ReducedMotion: true, SpinnerFrame: 1})
	second, _ := r.Render(c, Options{Width: 80, ReducedMotion: true, SpinnerFrame: 7})
	if first != second || !strings.Contains(ansi.Strip(first), "in_progress") {
		t.Fatal("reduced motion changed frames or hid running tool status")
	}
}

func TestNarrowFailedToolPreservesStatusBeforeLongTitle(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{{Tool: &app.ToolState{Title: strings.Repeat("long title ", 10), Status: "failed"}}}}
	rendered, _ := New().Render(c, Options{Width: 28})
	if !strings.Contains(ansi.Strip(rendered), "failed") {
		t.Fatal("long title hid failed tool status")
	}
}
