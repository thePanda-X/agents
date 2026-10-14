package transcript

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"agents/internal/app"
)

func TestLongTranscriptRetainsCachedWorkingSet(t *testing.T) {
	c := &app.Conversation{}
	for i := range 2050 {
		c.Entries = append(c.Entries, &app.Entry{ID: fmt.Sprint(i), Role: "assistant", Text: "A **response**.", Revision: 1})
	}
	r := New()
	opts := Options{Width: 80}
	_, positions := r.Render(c, opts)
	entry := c.Entries[0]
	value := r.cache[entry]
	// A sentinel in the cached block distinguishes reuse from an equivalent
	// re-render, without timing-dependent assertions or Markdown instrumentation.
	value.block = "  cached sentinel"
	r.cache[entry] = value
	text, nextPositions := r.Render(c, opts)
	if !strings.HasPrefix(text, "  cached sentinel") || len(r.cache) != len(c.Entries) {
		t.Fatal("long history flushed its active working set")
	}
	for i := range positions {
		if positions[i] != nextPositions[i] {
			t.Fatal("cached refresh changed entry row positions")
		}
	}
}

func TestCacheReleasesRemovedEntriesAndOtherConversations(t *testing.T) {
	r := New()
	first, second := &app.Entry{Role: "assistant", Text: "first"}, &app.Entry{Role: "assistant", Text: "second"}
	c := &app.Conversation{Entries: []*app.Entry{first, second}}
	r.Render(c, Options{Width: 80})
	c.Entries = []*app.Entry{second}
	r.Render(c, Options{Width: 80})
	if _, exists := r.cache[first]; exists || len(r.cache) != 1 {
		t.Fatal("cache retained entries removed during replay")
	}
	r.Render(&app.Conversation{Entries: []*app.Entry{first}}, Options{Width: 80})
	if _, exists := r.cache[second]; exists || len(r.cache) != 1 {
		t.Fatal("cache retained another conversation")
	}
	r.Render(nil, Options{Width: 80})
	if len(r.cache) != 0 {
		t.Fatal("empty view retained cached transcript")
	}
}

func TestUserCacheTracksTextDateAndLayout(t *testing.T) {
	entry := &app.Entry{Role: "user", Text: "original", Revision: 1}
	c := &app.Conversation{Entries: []*app.Entry{entry}}
	r := New()
	r.Render(c, Options{Width: 80})
	entry.Text = "replacement"
	entry.At = time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	got, _ := r.Render(c, Options{Width: 80, Compact: true})
	if !strings.Contains(got, "replacement") || r.cache[entry].compact != true || !r.cache[entry].at.Equal(entry.At) {
		t.Fatal("cached user block ignored text, date or layout changes")
	}
}

func BenchmarkCachedTranscriptRefresh(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("entries=%d", count), func(b *testing.B) {
			c := &app.Conversation{}
			for index := range count {
				role, text := "assistant", "A short **response** with some `code` and a second sentence.\n\nAnother paragraph."
				if index%2 == 0 {
					role, text = "user", "Explain the current implementation."
				}
				c.Entries = append(c.Entries, &app.Entry{ID: fmt.Sprint(index), Role: role, Text: text, Revision: 1})
			}
			r := New()
			opts := Options{Width: 100}
			r.Render(c, opts)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				r.Render(c, opts)
			}
		})
	}
}
