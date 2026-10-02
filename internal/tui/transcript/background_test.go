package transcript

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"agents/internal/app"

	"github.com/charmbracelet/x/ansi"
)

func TestUserMessageSurfaceCoversTextAndTimestampGaps(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, focused := range []bool{false, true} {
			for _, width := range []int{32, 120} {
				entry := &app.Entry{Role: "user", Text: "A short prompt\nMore text below", At: time.Date(2026, 10, 1, 15, 14, 0, 0, time.UTC)}
				content, _ := New().Render(&app.Conversation{Entries: []*app.Entry{entry}}, Options{Width: width, Compact: compact, Focused: focused})
				for _, line := range strings.Split(content, "\n") {
					assertSurfaceBackground(t, line)
				}
			}
		}
	}
}

func assertSurfaceBackground(t *testing.T, line string) {
	t.Helper()
	sgr := regexp.MustCompile(`\x1b\[([0-9;]*)m`)
	column, offset := 0, 0
	surface := false
	check := func(span string) {
		width := ansi.StringWidth(span)
		if width > 0 && column+width > 3 && !surface {
			t.Fatalf("unfilled message background at column %d: %q", column, line)
		}
		column += width
	}
	for _, match := range sgr.FindAllStringSubmatchIndex(line, -1) {
		check(line[offset:match[0]])
		params := strings.Split(line[match[2]:match[3]], ";")
		for i := 0; i < len(params); i++ {
			code, _ := strconv.Atoi(params[i])
			switch code {
			case 0, 49:
				surface = false
			case 38, 48:
				if i+4 < len(params) && params[i+1] == "2" {
					if code == 48 {
						surface = params[i+2] == "39" && params[i+3] == "39" && params[i+4] == "45"
					}
					i += 4
				}
			}
		}
		offset = match[1]
	}
	check(line[offset:])
}

func TestEmptyThoughtsExplainMissingTextInsteadOfBlankExpansion(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		entry := &app.Entry{Role: "thought", Text: " \n\t"}
		r := New()
		content, _ := r.Render(&app.Conversation{Entries: []*app.Entry{entry}}, Options{Width: 60, Thoughts: expanded})
		if !strings.Contains(ansi.Strip(content), "no text supplied by agent") {
			t.Fatal("empty thinking block still looks expandable to blank content")
		}
		if !strings.Contains(ansi.Strip(r.Detail(entry, 60, nil)), "No reasoning text") {
			t.Fatal("empty detail view lacks explanation")
		}
		entry.Text = "Actual reasoning text"
		content, _ = r.Render(&app.Conversation{Entries: []*app.Entry{entry}}, Options{Width: 60, Thoughts: true})
		if !strings.Contains(ansi.Strip(content), entry.Text) || strings.Contains(ansi.Strip(content), "no text supplied") {
			t.Fatal("real thinking text was hidden by empty-content fallback")
		}
	}
}
