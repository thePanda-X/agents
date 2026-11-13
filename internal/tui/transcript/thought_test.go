package transcript

import (
	"strings"
	"testing"
	"time"

	"agents/internal/app"

	"github.com/charmbracelet/x/ansi"
)

func TestThoughtLabelIncludesElapsedTime(t *testing.T) {
	start := time.Date(2026, 11, 12, 9, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{"seconds", 2 * time.Second, "▸ Thought · 2.0s"},
		{"minutes", 65 * time.Second, "▸ Thought · 1m 5s"},
		{"hours", 2*time.Hour + 3*time.Minute + 4*time.Second, "▸ Thought · 2h 3m 4s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &app.Conversation{Entries: []*app.Entry{
				{Role: "thought", Text: "plan", At: start},
				{Role: "assistant", Text: "done", At: start.Add(tc.elapsed)},
			}}
			content, _ := New().Render(c, Options{Width: 60})
			if !strings.Contains(ansi.Strip(content), tc.want) {
				t.Fatalf("thought label missing %q: %q", tc.want, ansi.Strip(content))
			}
		})
	}
}

func TestThoughtLabelFallsBackWithoutTimestamps(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{Role: "thought", Text: "plan"},
		{Role: "assistant", Text: "done"},
	}}
	content, _ := New().Render(c, Options{Width: 60})
	if !strings.Contains(ansi.Strip(content), "▸ Thinking") {
		t.Fatalf("replayed thought lost its label: %q", ansi.Strip(content))
	}
}

func TestThoughtDoesNotBorrowDurationAcrossTurns(t *testing.T) {
	start := time.Date(2026, 11, 12, 9, 0, 0, 0, time.Local)
	c := &app.Conversation{Entries: []*app.Entry{
		{Role: "thought", Text: "plan", At: start},
		{Role: "user", Text: "next task", At: start.Add(2 * time.Second)},
	}}
	content, _ := New().Render(c, Options{Width: 60})
	if got := ansi.Strip(content); !strings.Contains(got, "▸ Thinking") || strings.Contains(got, "Thought ·") {
		t.Fatalf("thought borrowed the next turn's timestamp: %q", got)
	}
}
