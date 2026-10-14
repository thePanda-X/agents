package transcript

import (
	"strings"
	"testing"
	"time"

	"agents/internal/app"
	"agents/internal/harness"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownCacheTracksRevisionsAndWidths(t *testing.T) {
	renderer := New()
	entry := &app.Entry{Role: "assistant", Kind: "message", Text: "## Heading\n\nOriginal text.", Revision: 1}
	c := &app.Conversation{Entries: []*app.Entry{entry}}
	first, _ := renderer.Render(c, Options{Width: 60})
	entry.Text += " Updated text."
	entry.Revision++
	second, _ := renderer.Render(c, Options{Width: 60})
	if first == second || !strings.Contains(ansi.Strip(second), "Updated text") {
		t.Fatal("cached Markdown hid a new revision")
	}
	if _, _ = renderer.Render(c, Options{Width: 30}); renderer.width != 30 {
		t.Fatal("renderer did not update wrap width")
	}
}

func TestFocusGutterKeepsLayoutStableAndSearchPreservesText(t *testing.T) {
	entry := &app.Entry{Role: "assistant", Text: "Unicode 界 needle and NEEDLE.\n\n" + strings.Repeat("long words ", 40), Revision: 1}
	c := &app.Conversation{Entries: []*app.Entry{entry}}
	renderer := New()
	unfocused, positions := renderer.Render(c, Options{Width: 40})
	focused, selectedPositions := renderer.Render(c, Options{Width: 40, Focused: true, Selected: 0, Search: "needle"})
	if lipgloss.Height(unfocused) != lipgloss.Height(focused) || positions[0] != selectedPositions[0] {
		t.Fatal("focus changed layout")
	}
	for _, line := range strings.Split(focused, "\n") {
		if ansi.StringWidth(line) > 40 {
			t.Fatal("focus gutter overflowed content")
		}
	}
	plain := strings.ReplaceAll(ansi.Strip(focused), "┃", " ")
	if strings.Join(strings.Fields(plain), " ") != strings.Join(strings.Fields(ansi.Strip(unfocused)), " ") {
		t.Fatal("search highlighting changed text")
	}
}

func TestCompactToolGroupsExpandForBrowseAndKeepFailuresAndSingletonsVisible(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{ID: "a", Tool: &app.ToolState{Title: "Read first", Status: "completed"}},
		{ID: "b", Tool: &app.ToolState{Title: "Read second", Status: "completed"}},
		{ID: "c", Tool: &app.ToolState{Title: "Failed command", Status: "failed"}},
		{ID: "d", Tool: &app.ToolState{Title: "Reported edit", Status: "completed", Diffs: []harness.Diff{{Path: "file.go"}}}},
	}}
	r := New()
	compact, positions := r.Render(c, Options{Width: 80, Compact: true})
	plain := ansi.Strip(compact)
	if !strings.Contains(plain, "2 successful tools") || strings.Contains(plain, "Read second") || !strings.Contains(plain, "Failed command") || !strings.Contains(plain, "Reported edit") || positions[0] != positions[1] {
		t.Fatal("incorrect compact tool grouping")
	}
	browse, positions := r.Render(c, Options{Width: 80, Compact: true, Focused: true})
	if !strings.Contains(ansi.Strip(browse), "Read second") || positions[0] == positions[1] {
		t.Fatal("browse did not expose grouped entries")
	}
}

func TestUltrawideProseUsesAvailableWidthAndCodeStillFits(t *testing.T) {
	r := newMarkdownRenderer(160)
	prose, err := r.Render(strings.Repeat("Readable prose words ", 50))
	if err != nil {
		t.Fatal(err)
	}
	proseWidth := 0
	for _, line := range strings.Split(prose, "\n") {
		if ansi.StringWidth(line) > 160 {
			t.Fatal("prose exceeds available width")
		}
		proseWidth = max(proseWidth, ansi.StringWidth(strings.TrimRight(ansi.Strip(line), " ")))
	}
	if proseWidth < 150 {
		t.Fatalf("prose wraps too early: widest content row is %d cells, available width is 160", proseWidth)
	}
	code, err := r.Render("```text\n" + strings.Repeat("x", 120) + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	wide := false
	for _, line := range strings.Split(code, "\n") {
		if ansi.StringWidth(line) > 160 {
			t.Fatal("code exceeds available width")
		}
		if ansi.StringWidth(line) > 96 {
			wide = true
		}
	}
	if !wide {
		t.Fatal("code was squeezed to prose width")
	}
}

func TestEntryPositionsMatchRenderedRows(t *testing.T) {
	for _, compact := range []bool{false, true} {
		c := &app.Conversation{Entries: []*app.Entry{
			{Role: "assistant", Text: "FIRST ENTRY"},
			{ID: "tool", Tool: &app.ToolState{Title: "SECOND ENTRY", Status: "completed"}},
			{ID: "thought", Role: "thought", Text: "Reasoning"},
			{Role: "assistant", Text: "FOURTH ENTRY"},
		}}
		content, positions := New().Render(c, Options{Width: 60, Compact: compact, Focused: true, Selected: 2, Expanded: map[string]bool{"thought2": true}})
		lines := strings.Split(ansi.Strip(content), "\n")
		for entry, marker := range []string{"FIRST ENTRY", "SECOND ENTRY", "Thinking", "FOURTH ENTRY"} {
			if positions[entry] >= len(lines) || !strings.Contains(lines[positions[entry]], marker) {
				t.Fatalf("entry %d position %d does not match rendered row (compact=%v)", entry, positions[entry], compact)
			}
		}
	}
}

func TestCompactSeparatesActivityFromResponsesWithoutSpacingActivityRows(t *testing.T) {
	for _, focused := range []bool{false, true} {
		c := &app.Conversation{Entries: []*app.Entry{
			{Role: "assistant", Text: "INTRO"},
			{ID: "thought", Role: "thought", Text: "Reasoning"},
			{ID: "a", Tool: &app.ToolState{Title: "Read first", Status: "completed"}},
			{ID: "b", Tool: &app.ToolState{Title: "Read second", Status: "completed"}},
			{Role: "assistant", Text: "FINAL"},
		}}
		content, positions := New().Render(c, Options{Width: 60, Compact: true, Focused: focused})
		lines := strings.Split(ansi.Strip(content), "\n")
		for _, index := range []int{1, 4} {
			if positions[index] < 2 || strings.TrimSpace(lines[positions[index]-1]) != "" || strings.TrimSpace(lines[positions[index]-2]) == "" {
				t.Fatalf("expected exactly one blank row at activity/response boundary (focused=%v)", focused)
			}
		}
		if focused && positions[2] != positions[1]+1 {
			t.Fatal("compact thinking/tool run gained extra spacing")
		}
		if !focused && positions[1] != positions[2] {
			t.Fatal("thinking did not join the compact tool group")
		}
		if !strings.Contains(lines[positions[4]], "FINAL") {
			t.Fatal("spacing broke response navigation index")
		}
		if focused && positions[3] != positions[2]+1 {
			t.Fatal("compact tool rows gained extra spacing")
		}
		if !focused && positions[2] != positions[3] {
			t.Fatal("successful tool grouping was lost")
		}
	}
}

func TestDiffIsInformationalAndSanitized(t *testing.T) {
	text := ansi.Strip(RenderDiff(harness.Diff{Path: "config.go", OldText: "old\n", NewText: "new\n"}))
	if !strings.Contains(text, "-old") || !strings.Contains(text, "+new") {
		t.Fatalf("incorrect unified diff: %q", text)
	}
}

func TestUserMessagesHaveBlankBoundariesAndAccuratePositions(t *testing.T) {
	user := &app.Entry{Role: "user", Text: "USER PROMPT\n" + strings.Repeat("wrapped words ", 12)}
	for _, compact := range []bool{false, true} {
		for _, focused := range []bool{false, true} {
			for _, width := range []int{20, 60} {
				for _, other := range []*app.Entry{
					{Role: "assistant", Text: "RESPONSE"},
					{Role: "thought", Text: "Reasoning"},
					{Tool: &app.ToolState{Title: "TOOL", Status: "completed"}},
					{Role: "user", Text: "ANOTHER PROMPT"},
				} {
					for _, entries := range [][]*app.Entry{{user, other}, {other, user}} {
						opts := Options{Width: width, Compact: compact, Focused: focused, Selected: 1}
						content, positions := New().Render(&app.Conversation{Entries: entries}, opts)
						first, _ := New().Render(&app.Conversation{Entries: entries[:1]}, opts)
						if positions[1] != lipgloss.Height(first)+1 {
							t.Fatalf("expected one separating row: positions=%v height=%d compact=%v focused=%v width=%d", positions, lipgloss.Height(first), compact, focused, width)
						}
						lines := strings.Split(ansi.Strip(content), "\n")
						if strings.TrimSpace(lines[positions[1]-1]) != "" {
							t.Fatal("user boundary is not blank")
						}
						secondOpts := opts
						secondOpts.Selected = 0
						second, _ := New().Render(&app.Conversation{Entries: entries[1:]}, secondOpts)
						if strings.Join(lines[positions[1]:], "\n") != ansi.Strip(second) {
							t.Fatal("entry position does not match rendered second block")
						}
					}
				}
			}
		}
	}
}

func TestCompactUserBoundariesPreserveToolGrouping(t *testing.T) {
	c := &app.Conversation{Entries: []*app.Entry{
		{Role: "user", Text: "FIRST PROMPT"},
		{Tool: &app.ToolState{Title: "Read first", Status: "completed"}},
		{Tool: &app.ToolState{Title: "Read second", Status: "completed"}},
		{Role: "user", Text: "NEXT PROMPT"},
	}}
	content, positions := New().Render(c, Options{Width: 60, Compact: true})
	lines := strings.Split(ansi.Strip(content), "\n")
	if positions[1] != positions[2] || positions[3] != positions[2]+2 {
		t.Fatal("user spacing broke grouped tool positions")
	}
	for _, index := range []int{1, 3} {
		if strings.TrimSpace(lines[positions[index]-1]) != "" {
			t.Fatal("missing blank boundary around grouped tools")
		}
	}
	if !strings.Contains(lines[positions[1]], "2 successful tools") || !strings.Contains(lines[positions[3]], "NEXT PROMPT") {
		t.Fatal("grouped tool or user navigation position is incorrect")
	}
}

func TestLongUserPromptWrapsWithoutLosingText(t *testing.T) {
	text := strings.Repeat("prompt words ", 25) + "finalword"
	entry := &app.Entry{Role: "user", Kind: "message", Text: text}
	rendered, _ := New().Render(&app.Conversation{Entries: []*app.Entry{entry}}, Options{Width: 40})
	if !strings.Contains(ansi.Strip(rendered), "finalword") {
		t.Fatal("long user prompt was truncated")
	}
}

func TestUserTimestampFitsOnFirstContentRowWithoutWrapping(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, focused := range []bool{false, true} {
			for _, width := range []int{32, 60, 120, 160} {
				entry := &app.Entry{Role: "user", Text: "A short prompt\n" + strings.Repeat("more words ", 30), At: time.Date(2026, 10, 1, 15, 14, 0, 0, time.Local)}
				content, _ := New().Render(&app.Conversation{Entries: []*app.Entry{entry}}, Options{Width: width, Compact: compact, Focused: focused})
				lines := strings.Split(ansi.Strip(content), "\n")
				found := false
				for _, line := range lines {
					if strings.Contains(line, "3:14 PM") {
						if !strings.Contains(line, "› A short prompt") {
							t.Fatal("timestamp separated from first prompt row")
						}
						found = true
					}
					if ansi.StringWidth(line) > width {
						t.Fatalf("message exceeds width %d: %q", width, line)
					}
				}
				if !found {
					t.Fatalf("timestamp wrapped or disappeared (width=%d compact=%v focused=%v):\n%s", width, compact, focused, content)
				}
				if strings.Count(strings.Join(lines, "\n"), "more") != 30 {
					t.Fatal("timestamp layout lost prompt text")
				}
			}
		}
	}
}

func TestHistoricalPromptWithoutTimestampDoesNotShowInventedTime(t *testing.T) {
	entry := &app.Entry{Role: "user", Text: "Historical prompt"}
	content, _ := New().Render(&app.Conversation{Entries: []*app.Entry{entry}}, Options{Width: 80})
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "Historical prompt") || strings.Contains(plain, "AM") || strings.Contains(plain, "PM") {
		t.Fatalf("historical prompt displays an invented time: %s", plain)
	}
}

func TestUserRailPersistsWithoutFocusAndFitsWrappedPrompts(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, width := range []int{10, 20, 60} {
			c := &app.Conversation{Entries: []*app.Entry{{Role: "user", Text: "USER PROMPT\n" + strings.Repeat("wrapped words ", 8)}}}
			opts := Options{Width: width, Compact: compact}
			content, _ := New().Render(c, opts)
			for _, line := range strings.Split(ansi.Strip(content), "\n") {
				if !strings.HasPrefix(line, "  │") {
					t.Fatalf("missing persistent user rail: %q", line)
				}
				if ansi.StringWidth(line) > width {
					t.Fatalf("user rail exceeds width %d: %q", width, line)
				}
			}
			opts.Focused = true
			focused, _ := New().Render(c, opts)
			if lipgloss.Height(content) != lipgloss.Height(focused) || !strings.HasPrefix(ansi.Strip(focused), "┃ │") {
				t.Fatal("selection gutter replaced user rail or changed layout")
			}
		}
	}
}

func TestTableRulesKeepFollowingEntryPositionAccurate(t *testing.T) {
	for _, compact := range []bool{false, true} {
		c := &app.Conversation{Entries: []*app.Entry{
			{Role: "assistant", Text: "| Item | Details |\n| --- | --- |\n| First | Many words that wrap inside the narrow table. |\n| Second | More details. |"},
			{Role: "thought", Text: "Next activity"},
		}}
		content, positions := New().Render(c, Options{Width: 32, Compact: compact})
		lines := strings.Split(ansi.Strip(content), "\n")
		if positions[1] >= len(lines) || !strings.Contains(lines[positions[1]], "Thinking") {
			t.Fatal("table separators broke the following entry's navigation index")
		}
	}
}
