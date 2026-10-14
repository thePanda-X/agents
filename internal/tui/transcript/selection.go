package transcript

import (
	"strings"

	"agents/internal/tui/theme"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TextPosition addresses a cell in the rendered transcript, not a byte in the
// Markdown source. Columns include the renderer's stable two-column gutter.
type TextPosition struct {
	Row, Column int
}

// TextSelection includes both endpoint cells, in either drag direction.
type TextSelection struct {
	Anchor, Head TextPosition
}

func (s TextSelection) Bounds() (TextPosition, TextPosition) {
	a, b := s.Anchor, s.Head
	if a.Row > b.Row || a.Row == b.Row && a.Column > b.Column {
		a, b = b, a
	}
	return a, b
}

// Text copies what was highlighted, including visual line breaks and code
// indentation, but without ANSI styles, the gutter, or right-hand padding.
func (s TextSelection) Text(content string) string {
	lines := strings.Split(content, "\n")
	start, end := s.Bounds()
	var selected []string
	for row := max(0, start.Row); row <= end.Row && row < len(lines); row++ {
		left, right := s.span(lines[row], row)
		selected = append(selected, strings.TrimRight(theme.Clean(ansi.Cut(lines[row], left, right)), " "))
	}
	return strings.Join(selected, "\n")
}

// Highlight changes only styling; row positions and cell widths stay intact.
func (s TextSelection) Highlight(content string, style lipgloss.Style) string {
	lines := strings.Split(content, "\n")
	start, end := s.Bounds()
	for row := max(0, start.Row); row <= end.Row && row < len(lines); row++ {
		line := lines[row]
		left, right := s.span(line, row)
		if left >= right {
			continue
		}
		// Override nested Markdown/code colors inside the selection, while cuts
		// outside it retain their original ANSI state.
		lines[row] = ansi.Cut(line, 0, left) +
			style.Render(ansi.Strip(ansi.Cut(line, left, right))) +
			ansi.Cut(line, right, ansi.StringWidth(line))
	}
	return strings.Join(lines, "\n")
}

func (s TextSelection) span(line string, row int) (int, int) {
	start, end := s.Bounds()
	plain := ansi.Strip(line)
	width := ansi.StringWidth(plain)
	left, right := min(2, width), width
	if row == start.Row {
		left, _ = cellBounds(plain, max(2, start.Column))
	}
	if row == end.Row {
		_, right = cellBounds(plain, max(2, end.Column))
	}
	return left, max(left, right)
}

// Snap either half of a wide glyph to the entire grapheme, including combining
// marks and joined emoji. Cutting midway through it would copy a padding space.
func cellBounds(line string, column int) (int, int) {
	position := 0
	for len(line) > 0 {
		cluster, width := ansi.FirstGraphemeCluster(line, ansi.GraphemeWidth)
		if column < position+width {
			return position, position + width
		}
		position += width
		line = line[len(cluster):]
	}
	return position, position
}
