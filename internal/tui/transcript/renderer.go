// Package transcript renders structured conversation entries and caches Markdown
// by entry revision. It has no transport or application mutation responsibilities.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/tui/theme"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/pmezard/go-difflib/difflib"
)

type cached struct {
	revision, seen uint64
	width, height  int
	compact        bool
	expanded       bool
	at             time.Time
	role, source   string
	text, block    string
}

type Renderer struct {
	cache        map[*app.Entry]cached
	markdown     *markdownRenderer
	width        int
	conversation *app.Conversation
	generation   uint64
}

func New() *Renderer { return &Renderer{cache: make(map[*app.Entry]cached)} }

// Detail renders a single entry without the transcript gutter or collapsed
// sections. Raw entry text remains authoritative for clipboard/editor exports.
func (r *Renderer) Detail(entry *app.Entry, width int, terminals map[string]string) string {
	if entry.Tool != nil {
		return r.tool(entry.Tool, true, terminals, max(10, width), 0)
	}
	text := theme.Clean(entry.Text)
	if entry.Role == "thought" && strings.TrimSpace(text) == "" {
		return theme.Quiet.Render("No reasoning text was supplied by the agent.")
	}
	if rendered, err := newMarkdownRenderer(max(10, width)).Render(text); err == nil {
		return strings.Trim(rendered, "\n")
	}
	return text
}

type Options struct {
	Width, Selected   int
	Focused, Thoughts bool
	Compact           bool
	SpinnerFrame      int
	Search            string
	SearchOccurrence  int
	ReducedMotion     bool
	Expanded          map[string]bool
	Terminals         map[string]string
}

func (r *Renderer) Render(c *app.Conversation, options Options) (string, []int) {
	if r.conversation != c {
		clear(r.cache)
		r.conversation = c
	}
	if c == nil || len(c.Entries) == 0 {
		clear(r.cache)
		return "", nil
	}
	width := max(10, options.Width)
	if r.width != width {
		r.markdown = newMarkdownRenderer(width - 2)
		r.width = width
		clear(r.cache)
	}
	// Retain the active transcript's working set, even for long histories. A
	// global entry-count flush made every refresh reparse all old Markdown.
	r.generation++
	defer r.pruneCache()
	blocks := make([]string, 0, len(c.Entries)*2)
	var search *regexp.Regexp
	if options.Search != "" {
		search = regexp.MustCompile("(?i)" + regexp.QuoteMeta(options.Search))
	}
	positions := make([]int, len(c.Entries))
	lines := 0
	skipUntil := 0
	for index, entry := range c.Entries {
		if index < skipUntil {
			continue
		}
		if options.Compact && index > 0 {
			previous := c.Entries[index-1]
			activity := func(e *app.Entry) bool { return e.Tool != nil || e.Role == "thought" }
			response := func(e *app.Entry) bool { return e.Tool == nil && e.Role == "assistant" }
			user := func(e *app.Entry) bool { return e.Tool == nil && e.Role == "user" }
			if user(previous) || user(entry) || activity(previous) && response(entry) || response(previous) && activity(entry) {
				// Separate sent prompts and model prose from surrounding activity,
				// while keeping activity runs dense. Include the spacer in row indexes.
				blocks = append(blocks, "")
				lines++
			}
		}
		positions[index] = lines
		// Compact composition groups thinking and successful tools, including
		// file changes. Active, failed and explicitly expanded entries stay visible.
		if options.Compact && !options.Focused {
			end := index
			tools, thoughts := 0, 0
			for end < len(c.Entries) {
				next := c.Entries[end]
				if next.Tool != nil {
					if next.Tool.Status != "completed" || options.Expanded[next.ID] {
						break
					}
					tools++
				} else if next.Role == "thought" {
					if options.Expanded[next.ID+fmt.Sprint(end)] {
						break
					}
					thoughts++
				} else {
					break
				}
				end++
			}
			if end-index >= 2 {
				var labels []string
				if tools > 0 {
					label := "successful tools"
					if tools == 1 {
						label = "successful tool"
					}
					labels = append(labels, fmt.Sprintf("%d %s", tools, label))
				}
				if thoughts > 0 {
					label := "thinking blocks"
					if thoughts == 1 {
						label = "thinking block"
					}
					labels = append(labels, fmt.Sprintf("%d %s", thoughts, label))
				}
				block := "  " + theme.Success.Render("▸ "+strings.Join(labels, " · ")) + theme.Quiet.Render(" · Tab to inspect")
				block = ansi.Truncate(block, width, "…")
				blocks = append(blocks, block)
				for member := index; member < end; member++ {
					positions[member] = lines
				}
				lines += lipgloss.Height(block)
				skipUntil = end
				continue
			}
		}
		var block string
		if entry.Tool != nil {
			frame := options.SpinnerFrame
			if options.ReducedMotion {
				frame = -1
			}
			block = r.tool(entry.Tool, options.Expanded[entry.ID], options.Terminals, width-2, frame)
		} else if entry.Role == "thought" {
			block = theme.Quiet.Render("◇ Thinking")
			expanded, overridden := options.Expanded[entry.ID+fmt.Sprint(index)]
			if !overridden {
				expanded = options.Thoughts
			}
			text := theme.Clean(entry.Text)
			if strings.TrimSpace(text) == "" {
				block += theme.Quiet.Render(" · no text supplied by agent")
				block = lipgloss.Wrap(block, width-2, " ")
			} else if expanded {
				block += "\n" + theme.Quiet.Render(lipgloss.Wrap(text, width-2, " "))
			} else {
				block += theme.Quiet.Render(" · collapsed")
			}
		} else {
			expanded := false
			if entry.Role == "user" && LargeText(entry.Text) {
				expanded = options.Expanded[entry.ID+fmt.Sprint(index)]
			}
			cacheEntry := r.message(entry, width, options.Compact, expanded)
			block = cacheEntry.text
			if search == nil && !(options.Focused && index == options.Selected) {
				blocks = append(blocks, cacheEntry.block)
				lines += cacheEntry.height + 1
				if options.Compact {
					lines--
				}
				continue
			}
		}
		gutter := ""
		if search != nil {
			current := -1
			if options.Focused && index == options.Selected {
				current = options.SearchOccurrence
			}
			block = highlightSearchMatch(block, search, current)
		}
		if options.Focused && index == options.Selected {
			gutter = theme.Accent.Render(strings.TrimSuffix(strings.Repeat("┃\n", max(1, lipgloss.Height(block))), "\n"))
		}
		// A stable gutter avoids rewrapping/clipping content when focus changes.
		block = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(2).Render(gutter), block)
		blocks = append(blocks, block)
		// Height already includes the first row. Two separating newlines add
		// one blank row; compact's single newline adds no blank row.
		lines += lipgloss.Height(block) + 1
		if options.Compact {
			lines--
		}
	}
	separator := "\n\n"
	if options.Compact {
		separator = "\n"
	}
	return strings.Join(blocks, separator), positions
}

func (r *Renderer) message(entry *app.Entry, width int, compact, expanded bool) cached {
	value, ok := r.cache[entry]
	if !ok || value.revision != entry.Revision || value.width != width ||
		value.compact != compact || value.expanded != expanded || value.source != entry.Text || value.role != entry.Role || !value.at.Equal(entry.At) {
		text := ""
		if entry.Role == "user" {
			text = renderUser(entry, width, compact, expanded)
		} else {
			text = theme.Clean(entry.Text)
			if rendered, err := r.markdown.Render(text); err == nil {
				text = rendered
			}
			text = strings.Trim(text, "\n")
		}
		block := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(2).Render(""), text)
		value = cached{revision: entry.Revision, width: width, compact: compact, expanded: expanded, at: entry.At,
			role: entry.Role, source: entry.Text, text: text, block: block, height: lipgloss.Height(block)}
	}
	value.seen = r.generation
	r.cache[entry] = value
	return value
}

func (r *Renderer) pruneCache() {
	for entry, value := range r.cache {
		if value.seen != r.generation {
			delete(r.cache, entry)
		}
	}
}

func renderUser(entry *app.Entry, width int, compact, expanded bool) string {
	// Reserve the gutter (2), left rail (1), and horizontal padding (2).
	innerWidth := width - 5
	text := entry.Text
	if LargeText(entry.Text) && !expanded {
		text = fmt.Sprintf("[%d lines · %d bytes · collapsed · Enter expands · o inspects]", strings.Count(entry.Text, "\n")+1, len(entry.Text))
	} else {
		text = theme.Clean(text)
	}
	text = lipgloss.Wrap(text, innerWidth-2, " ")
	first, rest, _ := strings.Cut(text, "\n")
	left := theme.Body.Background(theme.Surface).Bold(true).Render("› " + first)
	right := ""
	if !entry.At.IsZero() {
		right = theme.Quiet.Background(theme.Surface).Render(entry.At.Local().Format("3:04 PM"))
	}
	block := left
	if gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(right); right != "" && gap >= 2 {
		block += theme.Body.Background(theme.Surface).Render(strings.Repeat(" ", gap)) + right
	}
	if rest != "" {
		block += "\n" + rest
	}
	padding := 1
	if compact {
		padding = 0
	}
	return theme.Body.Background(theme.Surface).
		Border(lipgloss.Border{Left: "│"}, false, false, false, true).
		BorderForeground(theme.Pink).
		Padding(padding, 1).Width(width - 2).Render(block)
}

func highlightSearch(block string, search *regexp.Regexp) string {
	return highlightSearchMatch(block, search, -1)
}

func highlightSearchMatch(block string, search *regexp.Regexp, current int) string {
	lines := strings.Split(block, "\n")
	occurrence := 0
	for i, line := range lines {
		plain := ansi.Strip(line)
		matches := search.FindAllStringIndex(plain, -1)
		if len(matches) == 0 {
			continue
		}
		var b strings.Builder
		column := 0
		for _, match := range matches {
			start, end := ansi.StringWidth(plain[:match[0]]), ansi.StringWidth(plain[:match[1]])
			b.WriteString(ansi.Cut(line, column, start))
			style := theme.Accent.Background(theme.Selection).Bold(true)
			if occurrence == current {
				style = style.Reverse(true)
			}
			b.WriteString(style.Render(ansi.Cut(line, start, end)))
			occurrence++
			column = end
		}
		b.WriteString(ansi.Cut(line, column, ansi.StringWidth(line)))
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

var toolSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (r *Renderer) tool(t *app.ToolState, expanded bool, terminals map[string]string, width, spinnerFrame int) string {
	icon, statusStyle := "▸", theme.Quiet
	if expanded {
		icon = "▾"
	}
	switch t.Status {
	case "completed":
		statusStyle = theme.Success
	case "failed":
		statusStyle = theme.Error
	case "pending", "in_progress":
		icon, statusStyle = toolSpinnerFrames[max(0, spinnerFrame)%len(toolSpinnerFrames)], theme.Accent
		if spinnerFrame < 0 {
			icon = "◌"
		}
	}
	heading := theme.Line(statusStyle.Render(icon+" "+theme.Clean(t.Title)), statusStyle.Render(t.Status), width)
	if !expanded {
		return heading
	}
	parts := []string{heading}
	if len(t.Locations) > 0 {
		parts = append(parts, theme.Quiet.Render(theme.Clean(strings.Join(t.Locations, "\n"))))
	}
	if len(t.Input) > 0 {
		parts = append(parts, theme.Quiet.Render("Input"), pretty(t.Input))
	}
	if t.Text != "" {
		parts = append(parts, theme.Clean(t.Text))
	}
	for _, diff := range t.Diffs {
		parts = append(parts, RenderDiff(diff))
	}
	parts = append(parts, toolDiffPreviews(t)...)
	for _, id := range t.Terminals {
		if output := terminals[id]; output != "" {
			parts = append(parts, theme.Quiet.Render("Terminal output"), theme.Clean(output))
		}
	}
	if len(t.Output) > 0 {
		parts = append(parts, theme.Quiet.Render("Output"), pretty(t.Output))
	}
	return lipgloss.Wrap(strings.Join(parts, "\n"), width, " ")
}

func RenderDiff(diff harness.Diff) string {
	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{A: difflib.SplitLines(diff.OldText), B: difflib.SplitLines(diff.NewText), FromFile: "a/" + theme.Clean(diff.Path), ToFile: "b/" + theme.Clean(diff.Path), Context: 3})
	if err != nil {
		return theme.Error.Render("Cannot render diff: " + err.Error())
	}
	lines := strings.Split(theme.Clean(text), "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+"):
			lines[i] = theme.Success.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = theme.Error.Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = theme.Accent.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

func pretty(raw json.RawMessage) string {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, raw, "", "  "); err != nil {
		return theme.Clean(string(raw))
	}
	return theme.Clean(buffer.String())
}
