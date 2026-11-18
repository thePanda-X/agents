// Package transcript renders structured conversation entries and caches Markdown
// by entry revision. It has no transport or application mutation responsibilities.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
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
	clock24        bool
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
	stats        map[*harness.Diff]diffStat
}

func New() *Renderer {
	return &Renderer{cache: make(map[*app.Entry]cached), stats: make(map[*harness.Diff]diffStat)}
}

// Detail renders a single entry without the transcript gutter or collapsed
// sections. Raw entry text remains authoritative for clipboard/editor exports.
func (r *Renderer) Detail(entry *app.Entry, width int, terminals map[string]string) string {
	if entry.Tool != nil {
		return r.tool(entry, true, terminals, max(10, width), 0, true)
	}
	text := theme.Clean(entry.Text)
	if entry.Role == "thought" && strings.TrimSpace(text) == "" {
		return theme.Quiet.Render("No reasoning text was supplied by the agent.")
	}
	if rendered, err := newMarkdownRenderer(max(10, width)).Render(text); err == nil {
		return strings.Trim(NormalizeWidth(rendered, max(10, width)), "\n")
	}
	return NormalizeWidth(text, max(10, width))
}

type Options struct {
	Width, Selected   int
	Focused, Thoughts bool
	Compact           bool
	SpinnerFrame      int
	Search            string
	SearchOccurrence  int
	ReducedMotion     bool
	Clock24           bool
	Expanded          map[string]bool
	Terminals         map[string]string
}

func (r *Renderer) Render(c *app.Conversation, options Options) (string, []int) {
	if r.conversation != c {
		clear(r.cache)
		clear(r.stats)
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
		if index > 0 && spacerBefore(c.Entries[index-1], entry, options.Compact) {
			// Blocks are joined by single newlines; blank rows are explicit
			// spacer blocks so row indexes always match the rendered output.
			blocks = append(blocks, "")
			lines++
		}
		positions[index] = lines
		// Compact composition groups thinking and successful tools, including
		// file changes. Active, failed and explicitly expanded entries stay visible.
		if options.Compact && !options.Focused {
			end := index
			for end < len(c.Entries) {
				next := c.Entries[end]
				if next.Tool != nil {
					if next.Tool.Status != "completed" || options.Expanded[next.ID] {
						break
					}
				} else if next.Role == "thought" {
					if options.Expanded[next.ID+fmt.Sprint(end)] {
						break
					}
				} else {
					break
				}
				end++
			}
			if end-index >= 2 {
				block := "  " + theme.Success.Render("▸ "+activitySummary(c.Entries[index:end])) + theme.Quiet.Render(" · Tab to inspect")
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
			// While searching, show complete output so a match beyond the cap
			// is visible and highlighted where the search reveals it.
			block = r.tool(entry, options.Expanded[entry.ID], options.Terminals, width-2, frame, search != nil)
		} else if entry.Role == "thought" {
			// Thinking folds use the same ▸/▾ arrows as tools, so the open/closed
			// state reads identically across every foldable entry.
			label := "Thinking"
			if duration, ok := thoughtDuration(c.Entries, index); ok {
				label = "Thought · " + formatThoughtDuration(duration)
			}
			expanded, overridden := options.Expanded[entry.ID+fmt.Sprint(index)]
			if !overridden {
				expanded = options.Thoughts
			}
			text := theme.Clean(entry.Text)
			arrow := "▸ "
			if expanded && strings.TrimSpace(text) != "" {
				arrow = "▾ "
			}
			block = theme.Quiet.Render(arrow + label)
			if strings.TrimSpace(text) == "" {
				block += theme.Quiet.Render(" · no text supplied by agent")
				block = lipgloss.Wrap(block, width-2, " ")
			} else if expanded {
				block += "\n" + theme.Quiet.Render(lipgloss.Wrap(text, width-2, " "))
			}
			block = NormalizeWidth(block, width-2)
		} else {
			expanded := false
			if entry.Role == "user" && LargeText(entry.Text) {
				expanded = options.Expanded[entry.ID+fmt.Sprint(index)]
			}
			cacheEntry := r.message(entry, width, options.Compact, expanded, options.Clock24)
			block = cacheEntry.text
			if search == nil && !(options.Focused && index == options.Selected) {
				blocks = append(blocks, cacheEntry.block)
				lines += cacheEntry.height
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
		lines += lipgloss.Height(block)
	}
	return strings.Join(blocks, "\n"), positions
}

// activitySummary describes a compact group by what the agent did rather than
// how many entries it hides: "Read 3 files · 1 edit · ran 2 commands · 1
// thought". Kinds follow ACP's tool kinds; unknown kinds count as tools.
func activitySummary(entries []*app.Entry) string {
	counts := make(map[string]int)
	for _, entry := range entries {
		kind := "thought"
		if entry.Tool != nil {
			kind = entry.Tool.Kind
			if len(entry.Children) > 0 {
				kind = "agent"
			}
		}
		switch kind {
		case "read", "edit", "execute", "search", "fetch", "thought", "agent":
		case "think":
			kind = "thought"
		case "delete", "move":
			kind = "file"
		default:
			kind = "other"
		}
		counts[kind]++
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	}
	var parts []string
	for _, kind := range []string{"agent", "read", "search", "edit", "file", "execute", "fetch", "other", "thought"} {
		n := counts[kind]
		if n == 0 {
			continue
		}
		var part string
		switch kind {
		case "agent":
			part = fmt.Sprintf("%d %s", n, plural(n, "subagent", "subagents"))
		case "read":
			part = fmt.Sprintf("read %d %s", n, plural(n, "file", "files"))
		case "search":
			part = fmt.Sprintf("%d %s", n, plural(n, "search", "searches"))
		case "edit":
			part = fmt.Sprintf("%d %s", n, plural(n, "edit", "edits"))
		case "file":
			part = fmt.Sprintf("%d file %s", n, plural(n, "operation", "operations"))
		case "execute":
			part = fmt.Sprintf("ran %d %s", n, plural(n, "command", "commands"))
		case "fetch":
			part = fmt.Sprintf("%d %s", n, plural(n, "fetch", "fetches"))
		case "other":
			part = fmt.Sprintf("%d %s", n, plural(n, "tool", "tools"))
		case "thought":
			part = fmt.Sprintf("%d %s", n, plural(n, "thought", "thoughts"))
		}
		parts = append(parts, part)
	}
	summary := strings.Join(parts, " · ")
	return strings.ToUpper(summary[:1]) + summary[1:]
}

// spacerBefore decides whether a blank row separates two adjacent entries.
// Runs of tool calls and thinking stay dense in both layouts so a long agent
// turn does not double in height. Comfortable layout separates everything
// else; compact layout only separates sent prompts and model prose from the
// surrounding activity.
func spacerBefore(previous, entry *app.Entry, compact bool) bool {
	activity := func(e *app.Entry) bool { return e.Tool != nil || e.Role == "thought" }
	if activity(previous) && activity(entry) {
		return false
	}
	if !compact {
		return true
	}
	response := func(e *app.Entry) bool { return e.Tool == nil && e.Role == "assistant" }
	user := func(e *app.Entry) bool { return e.Tool == nil && e.Role == "user" }
	return user(previous) || user(entry) || activity(previous) && response(entry) || response(previous) && activity(entry)
}

func (r *Renderer) message(entry *app.Entry, width int, compact, expanded, clock24 bool) cached {
	value, ok := r.cache[entry]
	if !ok || value.revision != entry.Revision || value.width != width ||
		value.compact != compact || value.clock24 != clock24 || value.expanded != expanded || value.source != entry.Text || value.role != entry.Role || !value.at.Equal(entry.At) {
		text := ""
		if entry.Role == "user" {
			text = renderUser(entry, width, compact, expanded, clock24)
		} else {
			text = theme.Clean(entry.Text)
			if rendered, err := r.markdown.Render(text); err == nil {
				text = rendered
			}
			text = strings.Trim(text, "\n")
		}
		// Hard-break any line still wider than the content budget (long tokens,
		// code lines, wide tables, diffs) so the transcript never overflows its
		// viewport and horizontal scrolling stays impossible.
		text = NormalizeWidth(text, width-2)
		block := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(2).Render(""), text)
		value = cached{revision: entry.Revision, width: width, compact: compact, clock24: clock24, expanded: expanded, at: entry.At,
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

func renderUser(entry *app.Entry, width int, compact, expanded, clock24 bool) string {
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
		layout := "3:04 PM"
		if clock24 {
			layout = "15:04"
		}
		right = theme.Quiet.Background(theme.Surface).Render(entry.At.Local().Format(layout))
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
	return NormalizeWidth(theme.Body.Background(theme.Surface).
		Border(lipgloss.Border{Left: "│"}, false, false, false, true).
		BorderForeground(theme.Pink).
		Padding(padding, 1).Width(width-2).Render(block), width-2)
}

// thoughtDuration reports how long a reasoning block took, using the next
// timestamped entry as its end. Replayed sessions and turns that end without a
// following entry have no reliable duration, so the label stays "Thinking".
func thoughtDuration(entries []*app.Entry, index int) (time.Duration, bool) {
	start := entries[index].At
	if start.IsZero() {
		return 0, false
	}
	for _, next := range entries[index+1:] {
		if next.Role == "user" {
			return 0, false
		}
		if next.At.IsZero() || !next.At.After(start) {
			continue
		}
		return next.At.Sub(start), true
	}
	return 0, false
}

func formatThoughtDuration(duration time.Duration) string {
	if duration < 10*time.Second {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	seconds := max(int64(0), int64(duration/time.Second))
	var parts []string
	if hours := seconds / 3600; hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes := seconds / 60 % 60; minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if remainder := seconds % 60; remainder > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", remainder))
	}
	return strings.Join(parts, " ")
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

// NormalizeWidth hard-breaks any rendered line wider than width so transcript
// text never exceeds its viewport. Lines already within budget are returned
// unchanged, keeping existing wrapping and caches stable. ANSI styling and wide
// runes are preserved.
func NormalizeWidth(block string, width int) string {
	if width < 1 {
		return block
	}
	lines := strings.Split(block, "\n")
	changed := false
	for i, line := range lines {
		if ansi.StringWidth(line) <= width {
			continue
		}
		wrapped := ansi.Wrap(line, width, "")
		for _, part := range strings.Split(wrapped, "\n") {
			if ansi.StringWidth(part) > width {
				// Degenerate cases such as a single grapheme wider than the
				// limit: fall back to truncation rather than risking overflow.
				wrapped = ansi.Truncate(line, width, "")
				break
			}
		}
		lines[i] = wrapped
		changed = true
	}
	if !changed {
		return block
	}
	return strings.Join(lines, "\n")
}

var toolSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// toolOutputLimit bounds each tool output section in the transcript. The
// detail pager (o) and exports always show the complete text.
const toolOutputLimit = 30

// subagentTail bounds the subagent steps an expanded launch shows inline. The
// most recent steps are kept so a running subagent can be followed; the detail
// pager (o) shows all of them.
const subagentTail = 8

// subagentMessageLimit bounds each subagent message shown inline.
const subagentMessageLimit = 6

func active(t *app.ToolState) bool { return t.Status == "pending" || t.Status == "in_progress" }

func (r *Renderer) tool(entry *app.Entry, expanded bool, terminals map[string]string, width, spinnerFrame int, full bool) string {
	t := entry.Tool
	icon, statusStyle := "▸", theme.Quiet
	if expanded {
		icon = "▾"
	}
	status := theme.Quiet.Render(theme.Clean(t.Status))
	switch t.Status {
	case "completed":
		statusStyle = theme.Success
		status = r.diffStats(t)
	case "failed":
		statusStyle = theme.Error
		status = theme.Error.Render("failed")
	case "pending", "in_progress":
		icon, statusStyle = toolSpinnerFrames[max(0, spinnerFrame)%len(toolSpinnerFrames)], theme.Accent
		if spinnerFrame < 0 {
			icon = "◌"
		}
		status = theme.Accent.Render("running")
		if t.Status == "pending" {
			status = theme.Quiet.Render("pending")
		}
	}
	steps := 0
	for _, child := range entry.Children {
		if child.Tool != nil {
			steps++
		}
	}
	if steps > 0 {
		label := fmt.Sprintf("%d %s", steps, map[bool]string{true: "step", false: "steps"}[steps == 1])
		if ansi.Strip(status) == "" {
			status = theme.Quiet.Render(label)
		} else {
			status += theme.Quiet.Render(" · " + label)
		}
	}
	heading := theme.Line(statusStyle.Render(icon+" "+theme.Clean(t.Title)), status, width)
	if !expanded {
		// A collapsed running subagent shows what it is doing right now.
		if active(t) && len(entry.Children) > 0 {
			if latest := latestStep(entry.Children); latest != "" {
				heading += "\n" + ansi.Truncate(theme.Quiet.Render("  ⎿ "+latest), width, "…")
			}
		}
		return heading
	}
	section := func(text string) string {
		if full {
			return text
		}
		return capLines(text, toolOutputLimit)
	}
	parts := []string{heading}
	if len(t.Locations) > 0 {
		parts = append(parts, theme.Quiet.Render(theme.Clean(strings.Join(t.Locations, "\n"))))
	}
	if len(t.Input) > 0 {
		parts = append(parts, theme.Quiet.Render("Input"), section(pretty(t.Input)))
	}
	if len(entry.Children) > 0 {
		parts = append(parts, r.subagent(entry.Children, terminals, width, spinnerFrame, full))
	}
	if t.Text != "" {
		parts = append(parts, section(theme.Clean(t.Text)))
	}
	for _, diff := range t.Diffs {
		parts = append(parts, RenderDiff(diff))
	}
	parts = append(parts, toolDiffPreviews(t)...)
	for _, id := range t.Terminals {
		if output := terminals[id]; output != "" {
			parts = append(parts, theme.Quiet.Render("Terminal output"), section(theme.Clean(output)))
		}
	}
	if len(t.Output) > 0 {
		parts = append(parts, theme.Quiet.Render("Output"), section(pretty(t.Output)))
	}
	return NormalizeWidth(lipgloss.Wrap(strings.Join(parts, "\n"), width, " "), width)
}

// latestStep summarizes a subagent's most recent visible step on one line.
func latestStep(children []*app.Entry) string {
	for i := len(children) - 1; i >= 0; i-- {
		child := children[i]
		switch {
		case child.Tool != nil:
			return theme.Clean(child.Tool.Title)
		case child.Role == "thought":
			return "Thinking"
		case child.Role == "assistant":
			lines := strings.Split(strings.TrimSpace(theme.Clean(child.Text)), "\n")
			if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
				return last
			}
		}
	}
	return ""
}

// subagent renders a subagent's steps beneath its launch on a quiet rail.
// Inline, only the latest steps and the start of each message are shown;
// the full form shows every step, message, and nested subagent.
func (r *Renderer) subagent(children []*app.Entry, terminals map[string]string, width, spinnerFrame int, full bool) string {
	// The subagent's prompt is already shown as the launch input.
	visible := make([]*app.Entry, 0, len(children))
	for _, child := range children {
		if child.Tool != nil || child.Role != "user" {
			visible = append(visible, child)
		}
	}
	inner := max(8, width-2)
	var rows []string
	if hidden := len(visible) - subagentTail; !full && hidden > 0 {
		rows = append(rows, theme.Quiet.Render(fmt.Sprintf("… %d earlier %s · o in browse mode shows everything", hidden, map[bool]string{true: "step", false: "steps"}[hidden == 1])))
		visible = visible[hidden:]
	}
	for _, child := range visible {
		var block string
		switch {
		case child.Tool != nil:
			// Nested subagents open in the full form; other steps stay headings.
			block = r.tool(child, full && len(child.Children) > 0, terminals, inner, spinnerFrame, full)
		case child.Role == "thought":
			block = theme.Quiet.Render("Thinking")
			if text := strings.TrimSpace(theme.Clean(child.Text)); full && text != "" {
				block += "\n" + theme.Quiet.Render(text)
			}
		default:
			text := strings.TrimSpace(theme.Clean(child.Text))
			if text == "" {
				continue
			}
			if !full {
				text = capLines(text, subagentMessageLimit)
			}
			block = text
		}
		rows = append(rows, NormalizeWidth(lipgloss.Wrap(block, inner, " "), inner))
	}
	rail := theme.Quiet.Render("│ ")
	lines := strings.Split(strings.Join(rows, "\n"), "\n")
	for i, line := range lines {
		lines[i] = rail + line
	}
	return theme.Quiet.Render("Subagent") + "\n" + strings.Join(lines, "\n")
}

// capLines keeps the first limit lines of text and states how many were
// hidden, so one large command cannot flood the conversation.
func capLines(text string, limit int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= limit {
		return text
	}
	hidden := len(lines) - limit
	return strings.Join(lines[:limit], "\n") + "\n" + theme.Quiet.Render(fmt.Sprintf("… %d more lines · o in browse mode shows everything", hidden))
}

type diffStat struct {
	oldLen, newLen int
	added, removed int
}

// diffStats summarizes a completed tool's structured diffs as "+added −removed".
// Results are cached per diff because the transcript re-renders on every
// spinner tick while a turn is running.
func (r *Renderer) diffStats(t *app.ToolState) string {
	added, removed := 0, 0
	for i := range t.Diffs {
		diff := &t.Diffs[i]
		stat, ok := r.stats[diff]
		if !ok || stat.oldLen != len(diff.OldText) || stat.newLen != len(diff.NewText) {
			stat = diffStat{oldLen: len(diff.OldText), newLen: len(diff.NewText)}
			matcher := difflib.NewMatcher(diffLines(diff.OldText), diffLines(diff.NewText))
			for _, op := range matcher.GetOpCodes() {
				switch op.Tag {
				case 'r':
					stat.removed += op.I2 - op.I1
					stat.added += op.J2 - op.J1
				case 'd':
					stat.removed += op.I2 - op.I1
				case 'i':
					stat.added += op.J2 - op.J1
				}
			}
			r.stats[diff] = stat
		}
		added, removed = added+stat.added, removed+stat.removed
	}
	if added == 0 && removed == 0 {
		return ""
	}
	return theme.Success.Render(fmt.Sprintf("+%d", added)) + " " + theme.Error.Render(fmt.Sprintf("−%d", removed))
}

// diffLines splits text into newline-terminated lines. difflib.SplitLines
// appends a phantom empty line to text that already ends in a newline, which
// rendered as a blank context row at the end of every diff.
func diffLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	} else {
		lines[len(lines)-1] += "\n"
	}
	return lines
}

func RenderDiff(diff harness.Diff) string {
	// Git's a/ and b/ prefixes only make sense for relative paths; absolute
	// paths would otherwise render as "a//workspace/file.go".
	from, to := "a/"+theme.Clean(diff.Path), "b/"+theme.Clean(diff.Path)
	if strings.HasPrefix(diff.Path, "/") || filepath.IsAbs(diff.Path) {
		from, to = theme.Clean(diff.Path), theme.Clean(diff.Path)
	}
	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{A: diffLines(diff.OldText), B: diffLines(diff.NewText), FromFile: from, ToFile: to, Context: 3})
	if err != nil {
		return theme.Error.Render("Cannot render diff: " + err.Error())
	}
	// The unified diff ends with a newline; trimming it avoids blank rows
	// between a diff and whatever follows it.
	lines := strings.Split(strings.TrimRight(theme.Clean(text), "\n"), "\n")
	for i, line := range lines {
		switch {
		// File headers start with ---/+++ but are not removed/added lines.
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			lines[i] = theme.Quiet.Bold(true).Render(line)
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
