package transcript

import (
	"slices"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/tui/theme"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTextSelectionCopiesRenderedCellsInEitherDirection(t *testing.T) {
	content := "  Before " + theme.Accent.Bold(true).Render("selected") + " after  "
	for _, reverse := range []bool{false, true} {
		selection := TextSelection{Anchor: TextPosition{0, 9}, Head: TextPosition{0, 16}}
		if reverse {
			selection.Anchor, selection.Head = selection.Head, selection.Anchor
		}
		if got := selection.Text(content); got != "selected" {
			t.Fatalf("reverse=%v: copied %q", reverse, got)
		}
		highlighted := selection.Highlight(content, theme.TextSelection)
		if ansi.Strip(highlighted) != ansi.Strip(content) || lipgloss.Width(highlighted) != lipgloss.Width(content) {
			t.Fatal("highlighting changed rendered text or width")
		}
	}
}

func TestTextSelectionUsesProvidedStyleAndPreservesSurroundingStyles(t *testing.T) {
	content := "  " + theme.Accent.Bold(true).Underline(true).Render("before selected after")
	selection := TextSelection{Anchor: TextPosition{0, 9}, Head: TextPosition{0, 16}}
	for _, test := range []struct {
		name  string
		style lipgloss.Style
	}{
		{"graphite", theme.TextSelection},
		{"terminal", theme.TerminalTextSelection},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := selection.Highlight(content, test.style)
			want := ansi.Cut(content, 0, 9) + test.style.Render("selected") + ansi.Cut(content, 17, ansi.StringWidth(content))
			if got != want {
				t.Fatalf("selection did not override nested styles or preserve surrounding styles: got %q, want %q", got, want)
			}
			if selection.Text(got) != "selected" {
				t.Fatal("selection style changed copied text")
			}
		})
	}
}

func TestTextSelectionPreservesCodeIndentationAndSkipsGutterAndPadding(t *testing.T) {
	content := "┃ first line      \n┃   indented code   \n  \n┃ last line   "
	selection := TextSelection{Anchor: TextPosition{0, 2}, Head: TextPosition{3, 10}}
	if got, want := selection.Text(content), "first line\n  indented code\n\nlast line"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if highlighted := selection.Highlight(content, theme.TextSelection); ansi.Strip(highlighted) != ansi.Strip(content) || lipgloss.Height(highlighted) != 4 {
		t.Fatal("multiline selection changed transcript geometry")
	}
}

func TestTextSelectionNeverSplitsUnicodeGraphemes(t *testing.T) {
	for _, text := range []string{"界", "🙂", "👩‍💻", "e\u0301"} {
		content := "  a" + text + "z"
		width := ansi.StringWidth(text)
		for column := 3; column < 3+width; column++ {
			selection := TextSelection{Anchor: TextPosition{0, column}, Head: TextPosition{0, column}}
			if got := selection.Text(content); got != text {
				t.Fatalf("column %d split %q into %q", column, text, got)
			}
			if got := ansi.Strip(selection.Highlight(content, theme.TextSelection)); got != content {
				t.Fatalf("highlighting split %q: %q", text, got)
			}
		}
	}
}

func TestTextSelectionKeepsRenderedEntryPositions(t *testing.T) {
	conversation := &app.Conversation{Entries: []*app.Entry{
		{Role: "assistant", Text: "## First response\n\n" + strings.Repeat("Wrapped **prose** 界 words. ", 12)},
		{ID: "tool", Tool: &app.ToolState{Title: "Tool output", Status: "completed"}},
		{Role: "assistant", Text: "```go\n  fmt.Println(\"hi\")\n```\n\nLast response"},
	}}
	for _, compact := range []bool{false, true} {
		for _, width := range []int{28, 76} {
			renderer := New()
			options := Options{Width: width, Compact: compact, Focused: true}
			content, positions := renderer.Render(conversation, options)
			selection := TextSelection{Anchor: TextPosition{0, 2}, Head: TextPosition{positions[2] + 1, width - 1}}
			highlighted := selection.Highlight(content, theme.TextSelection)
			if ansi.Strip(highlighted) != ansi.Strip(content) || lipgloss.Height(highlighted) != lipgloss.Height(content) {
				t.Fatal("highlighting changed row positions")
			}
			_, next := renderer.Render(conversation, options)
			if !slices.Equal(positions, next) {
				t.Fatal("highlighting affected cached entry positions")
			}
			before, after := strings.Split(content, "\n"), strings.Split(highlighted, "\n")
			for row := range before {
				if ansi.StringWidth(before[row]) != ansi.StringWidth(after[row]) {
					t.Fatal("highlighting changed a row width")
				}
			}
		}
	}
}
