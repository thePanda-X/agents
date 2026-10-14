// Package overlays contains small, reusable overlay models with no application
// behavior. Selected values are interpreted by the root TUI model.
package overlays

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"agents/internal/tui/theme"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Item struct {
	ID, Label, Detail string
	Preview           string
	Value             any
	Keywords, Group   string
	Current           bool
	DisabledReason    string
}

type List struct {
	Title, Description string
	Items              []Item
	Search             textinput.Model
	Cursor             int
	Searchable         bool
	MultiSelect        bool
	Literal            bool
	// GroupHeaders adds nonselectable headings and keeps matches in item order.
	GroupHeaders bool
	Marked       map[string]bool
	Loading      bool
	Scroll       int
	EmptyMessage string
	Error, Hint  string
}

func NewList(title string, items []Item, searchable bool) *List {
	input := textinput.New()
	input.Placeholder, input.Prompt = "Filter…", "⌕ "
	input.SetWidth(50)
	style := input.Styles()
	style.Focused.Text, style.Focused.Prompt, style.Focused.Placeholder = theme.Body, theme.Accent, theme.Quiet
	input.SetStyles(style)
	input.Focus()
	return &List{Title: title, Items: items, Search: input, Searchable: searchable, Marked: make(map[string]bool)}
}

func (l *List) Filtered() []Item {
	query := strings.TrimSpace(l.Search.Value())
	type match struct {
		item  Item
		score int
	}
	var matches []match
	for _, item := range l.Items {
		if l.Literal && !strings.Contains(strings.ToLower(item.Label+" "+item.Detail), strings.ToLower(query)) {
			continue
		}
		if score, _, ok := fuzzyMatch(item.Label+" "+item.Detail+" "+item.Keywords+" "+item.Group, query); ok {
			if l.Literal {
				score = 0
			}
			matches = append(matches, match{item, score})
		}
	}
	if !l.GroupHeaders {
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	}
	result := make([]Item, len(matches))
	for i, match := range matches {
		result[i] = match.item
	}
	return result
}

// fuzzyMatch ranks exact/prefix/contiguous matches above subsequences. Positions
// are rune indices so highlighting never slices through a UTF-8 character.
func fuzzyMatch(text, query string) (int, []int, bool) {
	if query == "" {
		return 0, nil, true
	}
	textRunes, queryRunes := []rune(strings.ToLower(text)), []rune(strings.ToLower(query))
	var positions []int
	next, score := 0, 0
	for _, r := range queryRunes {
		found := -1
		for i := next; i < len(textRunes); i++ {
			if textRunes[i] == r {
				found = i
				break
			}
		}
		if found < 0 {
			return 0, nil, false
		}
		score += 10
		if found == next {
			score += 15
		}
		if found == 0 || unicode.IsSpace(textRunes[found-1]) || strings.ContainsRune("/_-.", textRunes[found-1]) {
			score += 20
		}
		score -= found - next
		positions = append(positions, found)
		next = found + 1
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, strings.ToLower(query)) {
		score += 200
	}
	if lower == strings.ToLower(query) {
		score += 400
	}
	if strings.Contains(lower, strings.ToLower(query)) {
		score += 100
	}
	return score, positions, true
}

func highlighted(text, query string) string {
	_, positions, _ := fuzzyMatch(text, query)
	matched := make(map[int]bool, len(positions))
	for _, index := range positions {
		matched[index] = true
	}
	var b strings.Builder
	for index, r := range []rune(text) {
		if matched[index] {
			b.WriteString(theme.Accent.Bold(true).Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (l *List) MarkedItems() []Item {
	var items []Item
	for _, item := range l.Items {
		if l.Marked[item.ID] {
			items = append(items, item)
		}
	}
	return items
}

func (l *List) Selected() (Item, bool) {
	items := l.Filtered()
	if len(items) == 0 {
		return Item{}, false
	}
	return items[min(max(l.Cursor, 0), len(items)-1)], true
}

func (l *List) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "up", "ctrl+k", "ctrl+p":
			l.Cursor = max(0, l.Cursor-1)
			return nil
		case "down", "ctrl+j", "ctrl+n":
			l.Cursor = min(max(0, len(l.Filtered())-1), l.Cursor+1)
			return nil
		case "pgup":
			l.Cursor = max(0, l.Cursor-10)
			return nil
		case "pgdown":
			l.Cursor = min(max(0, len(l.Filtered())-1), l.Cursor+10)
			return nil
		case "tab":
			if l.MultiSelect {
				if item, ok := l.Selected(); ok {
					l.Marked[item.ID] = !l.Marked[item.ID]
					l.Cursor = min(l.Cursor+1, max(0, len(l.Filtered())-1))
				}
				return nil
			}
		}
	}
	if l.Searchable {
		before := l.Search.Value()
		var cmd tea.Cmd
		l.Search, cmd = l.Search.Update(msg)
		if before != l.Search.Value() {
			l.Cursor = 0
			l.Scroll = 0
		}
		return cmd
	}
	return nil
}

func (l *List) View(width, height int) string {
	width, height = max(12, width), max(4, height)
	l.Cursor = min(max(l.Cursor, 0), max(0, len(l.Filtered())-1))
	// On wide terminals a bounded preview sits beside the results. Narrow
	// pickers retain the full result width instead of squeezing two columns.
	if item, ok := l.Selected(); ok && item.Preview != "" && width >= 84 {
		copy := *l
		copy.Items = append([]Item(nil), l.Items...)
		for i := range copy.Items {
			copy.Items[i].Preview = ""
		}
		leftWidth := width / 2
		left := copy.View(leftWidth, height)
		l.Scroll = copy.Scroll
		previewWidth := width - leftWidth - 3
		preview := item.Preview
		if item.DisabledReason != "" {
			preview = "Unavailable: " + item.DisabledReason + "\n\n" + preview
		}
		lines := strings.Split(lipgloss.Wrap(theme.Clean(preview), previewWidth, " "), "\n")
		if len(lines) > max(1, height-3) {
			lines = append(lines[:max(1, height-4)], "…")
		}
		right := theme.Heading.Render("Preview") + "\n\n" + theme.Body.Render(strings.Join(lines, "\n"))
		return lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(leftWidth).Render(left), " │ ", lipgloss.NewStyle().Width(previewWidth).Render(right))
	}
	search := l.Search
	search.SetWidth(max(8, width-4))
	// Headers, result rows, and the footer share a strict visual-row budget.
	// Keep rows single-line so terminal wrapping cannot hide the selection.
	oneLine := func(text string) string { return strings.Join(strings.Fields(theme.Clean(text)), " ") }
	footerRows := 2
	if height < 7 {
		footerRows = 1
	}
	searchRows := 0
	if l.Searchable {
		searchRows = 1
	}
	title := l.Title
	if item, ok := l.Selected(); ok && item.Group != "" {
		title += " · " + item.Group
	}
	parts := []string{theme.Heading.Render(ansi.Truncate(oneLine(title), width, "…"))}
	descriptionText := l.Description
	if l.Error != "" {
		descriptionText = l.Error
	}
	if descriptionText != "" {
		description := strings.Split(lipgloss.Wrap(theme.Clean(descriptionText), width, " "), "\n")
		limit := min(2, max(0, height-1-searchRows-1-footerRows-1))
		if len(description) > limit {
			description = description[:limit]
			if limit > 0 {
				description[limit-1] = ansi.Truncate(description[limit-1], max(1, width-1), "") + "…"
			}
		}
		if len(description) > 0 {
			style := theme.Quiet
			if l.Error != "" {
				style = theme.Error
			}
			parts = append(parts, style.Render(strings.Join(description, "\n")))
		}
	}
	if l.Searchable {
		parts = append(parts, search.View())
	}
	if height >= 7 {
		parts = append(parts, "")
	}
	items := l.Filtered()
	l.Cursor = min(max(l.Cursor, 0), max(0, len(items)-1))
	count := max(1, height-lipgloss.Height(strings.Join(parts, "\n"))-footerRows)
	// A scrolled group repeats its heading. With only one result row available,
	// prioritize the selected item; its group remains visible in the title.
	headers := l.GroupHeaders && count >= 2
	windowEnd := func(start int) int {
		rows, end := 0, start
		for end < len(items) {
			cost := 1
			if headers && items[end].Group != "" && (end == start || items[end].Group != items[end-1].Group) {
				cost++
			}
			if rows+cost > count {
				break
			}
			rows += cost
			end++
		}
		return end
	}
	// Scroll only when the cursor crosses the window edge. Clamp again after
	// filtering or resizing, and always keep the selected row visible.
	lastStart := max(0, len(items)-count)
	if headers {
		lastStart = max(0, len(items)-1)
	}
	l.Scroll = min(max(0, l.Scroll), lastStart)
	if l.Cursor < l.Scroll {
		l.Scroll = l.Cursor
	}
	for len(items) > 0 && l.Cursor >= windowEnd(l.Scroll) {
		l.Scroll++
	}
	if len(items) == 0 {
		message := "No matching results. Clear the filter."
		if l.Search.Value() == "" {
			message = l.EmptyMessage
			if message == "" {
				message = "No items available."
			}
		}
		if l.Loading {
			message = "Loading…"
		} else if l.Error != "" {
			message = "Enter to retry."
		}
		parts = append(parts, theme.Quiet.Render(ansi.Truncate(message, width, "…")))
	}
	start := l.Scroll
	end := windowEnd(start)
	for index := start; index < end; index++ {
		item := items[index]
		if headers && item.Group != "" && (index == start || item.Group != items[index-1].Group) {
			parts = append(parts, theme.Heading.Render(ansi.Truncate("─ "+oneLine(item.Group)+" ─", width, "…")))
		}
		label := oneLine(item.Label)
		prefix := "  "
		if index == l.Cursor {
			prefix = "› "
		}
		if l.MultiSelect {
			mark := "[ ] "
			if l.Marked[item.ID] {
				mark = "[x] "
			}
			prefix += mark
		} else if item.Current {
			prefix += "✓ "
		}
		line := highlighted(label, l.Search.Value())
		if item.Detail != "" {
			detailWidth := max(4, min(24, width/3))
			detail := theme.Quiet.Render(ansi.Truncate(highlighted(oneLine(item.Detail), l.Search.Value()), detailWidth, "…"))
			line = theme.Line(line, detail, width-lipgloss.Width(prefix))
		}
		line = prefix + ansi.Truncate(line, width-lipgloss.Width(prefix), "…")
		if index == l.Cursor {
			line = theme.Body.Background(theme.Selection).Width(width).Render(line)
		} else if item.DisabledReason != "" {
			line = theme.Quiet.Render(line)
		}
		parts = append(parts, line)
	}
	position := 0
	if len(items) > 0 {
		position = min(l.Cursor+1, len(items))
	}
	hint := "↑↓ / Ctrl+N/P select · Enter confirm · Esc close"
	if l.MultiSelect {
		hint = "Tab mark · Enter attach · Esc close"
	}
	if l.Hint != "" {
		hint = l.Hint
	}
	if width < 60 {
		hint = "Enter · Esc close"
		if l.MultiSelect {
			hint = "Tab mark · Enter · Esc"
		} else if l.Error != "" {
			hint = "Enter retry · Esc close"
		} else if strings.Contains(l.Hint, "insert") {
			hint = "Enter insert · Esc"
		} else if strings.Contains(l.Hint, "toggle") {
			hint = "Enter toggle · Esc"
		} else if strings.Contains(l.Hint, "Del") {
			hint = "Del remove · Esc close"
		}
	}
	if footerRows == 2 {
		parts = append(parts, "")
	}
	parts = append(parts, theme.Line(theme.Quiet.Render(fmt.Sprintf("%d/%d", position, len(items))), theme.Quiet.Render(hint), width))
	return strings.Join(parts, "\n")
}
