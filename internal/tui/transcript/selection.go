package transcript

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"agents/internal/app"
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

// Source copies the text that actually arrived instead of its rendering: no soft
// wraps, table borders, gutters or Markdown styling. Message entries are located
// in their raw text by matching letters and digits only (rendering only adds or
// drops punctuation and whitespace); anything that cannot be located, such as
// tools or thinking, falls back to the rendered cells.
// ponytail: skeleton match, so a selection of pure punctuation or one that spans
// HTML entities/code-fence languages copies rendered text. Exact source maps
// would need Glamour to expose them.
func (s TextSelection) Source(content string, entries []*app.Entry, positions []int) string {
	if len(positions) == 0 || len(positions) != len(entries) {
		return s.Text(content)
	}
	lines := strings.Split(content, "\n")
	start, end := s.Bounds()
	type part struct {
		entry int
		rows  []string
		hint  int // letters/digits rendered before the selection in its entry
	}
	var parts []part
	for row := max(0, start.Row); row <= end.Row && row < len(lines); row++ {
		index := entryAt(positions, row)
		left, right := s.span(lines[row], row)
		if len(parts) == 0 || parts[len(parts)-1].entry != index {
			parts = append(parts, part{entry: index})
			if len(parts) == 1 && index < len(entries) {
				var before []string
				for r := positions[index]; r < row; r++ {
					before = append(before, ansi.Strip(lines[r]))
				}
				before = append(before, ansi.Strip(ansi.Cut(lines[row], 0, left)))
				skeleton, _ := skeleton(clock(entries[index], lines[row], strings.Join(before, "\n")))
				parts[0].hint = len(skeleton)
			}
		}
		text := strings.TrimRight(theme.Clean(ansi.Cut(lines[row], left, right)), " ")
		if index < len(entries) {
			text = clock(entries[index], lines[row], text)
		}
		last := &parts[len(parts)-1]
		last.rows = append(last.rows, text)
	}
	var out []string
	for i, p := range parts {
		rendered := strings.Trim(strings.Join(p.rows, "\n"), "\n")
		if p.entry >= len(entries) || entries[p.entry].Tool != nil || entries[p.entry].Role == "thought" {
			out = append(out, rendered)
			continue
		}
		raw := entries[p.entry].Text
		if i > 0 && i < len(parts)-1 {
			out = append(out, raw)
			continue
		}
		out = append(out, locate(raw, rendered, p.hint))
	}
	return strings.Join(out, "\n\n")
}

// locate returns the part of raw matching rendered, or rendered itself.
func locate(raw, rendered string, hint int) string {
	want, _ := skeleton(rendered)
	have, offsets := skeleton(raw)
	best := -1
	for i := 0; i+len(want) <= len(have) && len(want) > 0; i++ {
		if slices.Equal(have[i:i+len(want)], want) && (best < 0 || abs(i-hint) < abs(best-hint)) {
			best = i
		}
	}
	if best < 0 {
		return rendered
	}
	if best == 0 && len(want) == len(have) {
		return raw
	}
	last := offsets[best+len(want)-1]
	_, width := utf8.DecodeRuneInString(raw[last:])
	return raw[offsets[best] : last+width]
}

func abs(n int) int { return max(n, -n) }

func skeleton(text string) ([]rune, []int) {
	var runes []rune
	var offsets []int
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			runes, offsets = append(runes, unicode.ToLower(r)), append(offsets, i)
		}
	}
	return runes, offsets
}

// clock drops the right-aligned send time on a user message's first row.
func clock(entry *app.Entry, line, text string) string {
	if entry.Role != "user" || entry.At.IsZero() || !strings.Contains(ansi.Strip(line), "› ") {
		return text
	}
	return strings.TrimRight(strings.TrimSuffix(strings.TrimRight(text, " "), entry.At.Local().Format("3:04 PM")), " ")
}

// entryAt returns the last entry starting at or above row; blank separator rows
// belong to the entry before them.
func entryAt(positions []int, row int) int {
	index := 0
	for i, position := range positions {
		if position <= row {
			index = i
		}
	}
	return index
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
