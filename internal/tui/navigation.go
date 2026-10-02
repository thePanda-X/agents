package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"agents/internal/app"
	"agents/internal/tui/overlays"
	"agents/internal/tui/theme"
	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) setTranscriptFocus(focused bool) tea.Cmd {
	changed := m.focusedTranscript != focused
	follow, anchor, offset := m.viewport.AtBottom(), 0, 0
	if changed && m.opts.Settings.Compact {
		previous := -1
		for index, position := range m.positions {
			if position > m.viewport.YOffset() {
				break
			}
			if position != previous {
				anchor, offset = index, m.viewport.YOffset()-position
			}
			previous = position
		}
	}
	m.focusedTranscript, m.dirty = focused, true
	m.browsePrefix = ""
	if focused {
		if !m.browseVisited {
			if c := m.state.Active(); c != nil {
				m.selected = max(0, len(c.Entries)-1)
			}
			m.browseVisited = true
		}
		m.input.Blur()
	}
	if changed && m.opts.Settings.Compact {
		m.refresh()
		if follow {
			m.viewport.GotoBottom()
		} else if anchor < len(m.positions) {
			start := m.positions[anchor]
			for _, next := range m.positions[anchor+1:] {
				if next > start {
					offset = min(offset, max(0, next-start-1))
					break
				}
			}
			m.viewport.SetYOffset(start + offset)
		}
	}
	if focused && changed {
		m.refresh()
		if m.selected < len(m.positions) {
			m.browseLine = min(max(m.positions[m.selected], m.viewport.YOffset()), m.viewport.YOffset()+m.viewport.Height()-1)
		}
	}
	if focused {
		return nil
	}
	return m.input.Focus()
}

func (m *Model) scrollTranscript(down bool) {
	delta := max(1, m.viewport.Height()/2)
	if !down {
		delta = -delta
	}
	m.scrollTranscriptLines(delta)
}

func (m *Model) scrollTranscriptLines(delta int) {
	// Refresh pending layout changes (including expanded tools) before scrolling.
	// Moving by visual rows must not snap back to an entry's first line.
	if m.dirty {
		m.refresh()
		m.dirty = false
	}
	total := len(strings.Split(m.viewport.GetContent(), "\n"))
	// Track a reading line independently from entry boundaries. It moves even
	// when the viewport is clamped at the top/bottom, so short tool/thought
	// headers in the same viewport can still become the active entry.
	if m.focusedTranscript {
		m.browseLine = min(max(m.browseLine, m.viewport.YOffset()), m.viewport.YOffset()+m.viewport.Height()-1)
		m.browseLine = min(max(0, m.browseLine+delta), max(0, total-1))
	}
	m.viewport.SetYOffset(m.viewport.YOffset() + delta)
	if m.focusedTranscript && len(m.positions) > 0 {
		selected := 0
		for index, position := range m.positions {
			if position > m.browseLine {
				break
			}
			selected = index
		}
		if selected != m.selected {
			m.selected, m.dirty = selected, true
		}
	}
	if m.viewport.AtBottom() {
		m.unseen = false
	}
}

func (m *Model) browseKey(key string) (tea.Cmd, bool) {
	prefix := m.browsePrefix
	m.browsePrefix = ""
	switch key {
	case "j":
		m.scrollTranscriptLines(1)
	case "k":
		m.scrollTranscriptLines(-1)
	case "i":
		return m.setTranscriptFocus(false), true
	case "/":
		m.openTranscriptSearch()
	case "n":
		m.nextSearch(1)
	case "N":
		m.nextSearch(-1)
	case "?":
		m.openHelp()
	case "g":
		if prefix == "g" && time.Since(m.browsePrefixAt) < time.Second {
			m.selectEntry(0)
			m.viewport.GotoTop()
			m.browseLine = 0
		} else {
			m.browsePrefix, m.browsePrefixAt = "g", time.Now()
		}
	case "G":
		if c := m.state.Active(); c != nil {
			m.selectEntry(len(c.Entries) - 1)
			m.viewport.GotoBottom()
			m.browseLine = max(0, len(strings.Split(m.viewport.GetContent(), "\n"))-1)
			m.unseen = false
		}
	case "J":
		m.jumpEntry(1, "turn")
	case "K":
		m.jumpEntry(-1, "turn")
	case "]":
		m.jumpEntry(1, "failed")
	case "[":
		m.jumpEntry(-1, "failed")
	case "}":
		m.jumpEntry(1, "edit")
	case "{":
		m.jumpEntry(-1, "edit")
	case "o":
		m.inspectSelected()
	case "c":
		m.openCodeBlocks()
	case "e":
		return m.openReportedFile(), true
	case "v":
		return m.exportSelected(), true
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) selectedEntry() *app.Entry {
	c := m.state.Active()
	if c == nil || m.selected < 0 || m.selected >= len(c.Entries) {
		return nil
	}
	return c.Entries[m.selected]
}

func (m *Model) selectEntry(index int) {
	if c := m.state.Active(); c == nil || len(c.Entries) == 0 {
		return
	}
	m.moveSelection(index - m.selected)
}

func (m *Model) jumpEntry(direction int, kind string) {
	c := m.state.Active()
	if c == nil {
		return
	}
	for i := m.selected + direction; i >= 0 && i < len(c.Entries); i += direction {
		e := c.Entries[i]
		match := kind == "turn" && e.Role == "user" || kind == "failed" && e.Tool != nil && e.Tool.Status == "failed" || kind == "edit" && e.Tool != nil && len(e.Tool.Diffs) > 0
		if match {
			m.selectEntry(i)
			return
		}
	}
	m.state.Notice = "No more matching entries"
}

func (m *Model) entryText(entry *app.Entry) string {
	if entry.Tool == nil {
		return entry.Text
	}
	t := entry.Tool
	parts := []string{t.Title, t.Status, strings.Join(t.Locations, "\n"), "Input", string(t.Input), t.Text, "Output", string(t.Output)}
	for _, diff := range t.Diffs {
		parts = append(parts, transcript.RenderDiff(diff))
	}
	for _, id := range t.Terminals {
		parts = append(parts, m.state.TerminalOutputs[id])
	}
	return theme.Clean(strings.Join(parts, "\n\n"))
}

func (m *Model) openTranscriptSearch() {
	var items []overlays.Item
	if c := m.state.Active(); c != nil {
		for i, entry := range c.Entries {
			text := theme.Clean(m.entryText(entry))
			items = append(items, overlays.Item{ID: fmt.Sprint(i), Label: fmt.Sprintf("%d · %s", i+1, entry.Role), Detail: strings.Join(strings.Fields(text), " "), Preview: text, Value: i})
		}
	}
	m.openList("search", "Search transcript", items)
	m.list.Literal = true
	m.list.Search.SetValue(m.searchQuery)
	m.list.Description = "Search response text, tools, paths, diffs, and output · Enter jumps · n/N repeats in browse mode"
	m.list.EmptyMessage = "No conversation text to search yet."
}

func (m *Model) nextSearch(direction int) {
	if m.searchQuery == "" {
		m.openTranscriptSearch()
		return
	}
	c := m.state.Active()
	if c == nil || len(c.Entries) == 0 {
		return
	}
	if m.selected == m.searchEntry {
		next := m.searchOccurrence + direction
		if next >= 0 && next < m.searchOccurrences {
			m.revealSearchOccurrence(m.selected, next)
			return
		}
	}
	for step := 1; step <= len(c.Entries); step++ {
		i := (m.selected + direction*step + len(c.Entries)) % len(c.Entries)
		if strings.Contains(strings.ToLower(m.entryText(c.Entries[i])), strings.ToLower(m.searchQuery)) {
			occurrence := 0
			if direction < 0 {
				occurrence = -1
			}
			m.revealSearchOccurrence(i, occurrence)
			return
		}
	}
	m.state.Notice = "No search matches"
}

func (m *Model) revealSearch(index int) {
	m.revealSearchOccurrence(index, 0)
}

func (m *Model) revealSearchOccurrence(index, occurrence int) {
	m.selected = index
	entry := m.selectedEntry()
	if entry == nil {
		return
	}
	if entry.Tool != nil {
		m.expanded[entry.ID] = true
	}
	if entry.Role == "thought" {
		m.expanded[entry.ID+fmt.Sprint(index)] = true
	}
	m.refresh()
	if index >= len(m.positions) {
		return
	}
	lines := strings.Split(m.viewport.GetContent(), "\n")
	start, end := m.positions[index], len(lines)
	if index+1 < len(m.positions) {
		end = m.positions[index+1]
	}
	query := regexp.MustCompile("(?i)" + regexp.QuoteMeta(m.searchQuery))
	var hits []int
	for line := start; line < min(end, len(lines)); line++ {
		if m.searchQuery != "" {
			for range query.FindAllStringIndex(theme.Clean(lines[line]), -1) {
				hits = append(hits, line)
			}
		}
	}
	m.searchEntry, m.searchOccurrences = index, len(hits)
	if len(hits) > 0 {
		if occurrence < 0 {
			occurrence = len(hits) - 1
		}
		m.searchOccurrence = min(occurrence, len(hits)-1)
		m.refresh()
		line := hits[m.searchOccurrence]
		m.viewport.SetYOffset(max(start, line-1))
		m.browseLine = line
		m.state.Notice = fmt.Sprintf("Search %q · match %d/%d in entry %d · n/N next/previous", m.searchQuery, m.searchOccurrence+1, len(hits), index+1)
		return
	}
	m.viewport.SetYOffset(start)
	m.browseLine = start
}

func (m *Model) openHistory() {
	var items []overlays.Item
	seen := make(map[string]bool)
	for i := len(m.history) - 1; i >= 0; i-- {
		text := m.history[i]
		if seen[text] {
			continue
		}
		seen[text] = true
		items = append(items, overlays.Item{ID: fmt.Sprint(i), Label: strings.Join(strings.Fields(text), " "), Preview: text, Value: text})
	}
	m.openList("history", "Prompt history", items)
	m.list.EmptyMessage = "No prompts sent in this conversation yet."
	m.list.Description = "Enter replaces the draft; it never sends a prompt. Current draft is retained for Alt+Down."
}

func (m *Model) inspectSelected() {
	entry := m.selectedEntry()
	if entry == nil {
		return
	}
	m.openInfo("Entry details · "+entry.Role, m.renderer.Detail(entry, m.info.Width(), m.state.TerminalOutputs))
	if entry.Tool != nil {
		m.infoPaths = append([]string(nil), entry.Tool.Locations...)
		for _, diff := range entry.Tool.Diffs {
			m.infoPaths = append(m.infoPaths, diff.Path)
		}
	}
}

func (m *Model) openCodeBlocks() {
	entry := m.selectedEntry()
	if entry == nil {
		return
	}
	var items []overlays.Item
	var fence, language string
	var lines []string
	for _, line := range strings.Split(entry.Text, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence == "" {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				char := trimmed[0]
				length := 0
				for length < len(trimmed) && trimmed[length] == char {
					length++
				}
				fence, language = trimmed[:length], strings.TrimSpace(trimmed[length:])
				lines = nil
			}
		} else if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])) == "" {
			text := strings.Join(lines, "\n")
			items = append(items, overlays.Item{ID: fmt.Sprint(len(items)), Label: fmt.Sprintf("Block %d · %s", len(items)+1, language), Detail: strings.Join(strings.Fields(text), " "), Preview: text, Value: text})
			fence = ""
		} else {
			lines = append(lines, line)
		}
	}
	m.openList("code", "Copy code block", items)
	m.list.Description = "Enter copies raw code without Markdown fences (terminal clipboard required)."
}

func (m *Model) jumpHunk(direction int) {
	current := m.info.YOffset()
	if direction > 0 {
		for _, line := range m.infoHunks {
			if line > current {
				m.info.SetYOffset(line)
				return
			}
		}
	} else {
		for i := len(m.infoHunks) - 1; i >= 0; i-- {
			if m.infoHunks[i] < current {
				m.info.SetYOffset(m.infoHunks[i])
				return
			}
		}
	}
}
