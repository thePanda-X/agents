package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"agents/internal/tui/overlays"
	"agents/internal/tui/theme"
	"agents/internal/tui/transcript"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

const (
	maxDraftBytes  = 8 << 20
	maxPasteBlocks = 128
)

type pastedText struct {
	ID           uint64
	Marker, Text string
}

type pasteSpan struct {
	start, end int
}

// Markers are presentation only. Expansion is a single pass, so marker-shaped
// text inside a pasted block stays literal rather than recursively expanding.
func expandPastes(display string, pastes []pastedText) string {
	if len(pastes) == 0 {
		return display
	}
	pairs := make([]string, 0, len(pastes)*2)
	for _, paste := range pastes {
		pairs = append(pairs, paste.Marker, paste.Text)
	}
	return strings.NewReplacer(pairs...).Replace(display)
}

func expandedBytes(display string, pastes []pastedText) int {
	size := len(display)
	for _, paste := range pastes {
		size += strings.Count(display, paste.Marker) * (len(paste.Text) - len(paste.Marker))
	}
	return size
}

func (m *Model) draftText() string {
	return expandPastes(m.input.Value(), m.currentDraft().Pastes)
}

func (m *Model) newPaste(text, display string) pastedText {
	for {
		m.nextPasteID++
		marker := fmt.Sprintf("[Paste #%d: %d lines]", m.nextPasteID, strings.Count(text, "\n")+1)
		if !strings.Contains(display, marker) && !strings.Contains(text, marker) {
			return pastedText{ID: m.nextPasteID, Marker: marker, Text: text}
		}
	}
}

// Full-text restoration never feeds a huge block through textarea wrapping.
// Persistence, history, recovery and the editor all use this boundary.
func (m *Model) setDraftText(text string) bool {
	if len(text) > maxDraftBytes || !utf8.ValidString(text) {
		m.state.Notice = "Draft exceeds 8 MiB or is not UTF-8 · current draft preserved"
		return false
	}
	buffer := m.currentDraft()
	display := text
	var pastes []pastedText
	if transcript.LargeText(text) {
		paste := m.newPaste(text, "")
		display, pastes = paste.Marker, []pastedText{paste}
	}
	if m.input.CharLimit > 0 && utf8.RuneCountInString(display) > m.input.CharLimit {
		m.state.Notice = "Draft exceeds composer limit · current draft preserved"
		return false
	}
	buffer.Pastes, buffer.Text, buffer.Loaded = pastes, display, true
	m.setComposerAt(display, len(display))
	return true
}

func pasteSpans(display string, pastes []pastedText) []pasteSpan {
	var spans []pasteSpan
	for _, paste := range pastes {
		for offset := 0; offset < len(display); {
			index := strings.Index(display[offset:], paste.Marker)
			if index < 0 {
				break
			}
			start := offset + index
			spans = append(spans, pasteSpan{start, start + len(paste.Marker)})
			offset = start + len(paste.Marker)
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	return spans
}

func positionOffset(text string, position textarea.Position) int {
	offset := 0
	for row, line := range strings.Split(text, "\n") {
		if row == position.Row {
			column := 0
			for index := range line {
				if column == position.Col {
					return offset + index
				}
				column++
			}
			return offset + len(line)
		}
		offset += len(line) + 1
	}
	return len(text)
}

func (m *Model) composerOffset(text string) int {
	return positionOffset(text, textarea.Position{Row: m.input.Line(), Col: m.input.Column()})
}

func (m *Model) setComposerAt(text string, offset int) {
	// Bubbles truncates insertion silently at CharLimit. Callers check limits
	// first; temporarily disable it when rebuilding an already validated buffer.
	limit := m.input.CharLimit
	m.input.CharLimit = 0
	m.input.SetValue(text)
	m.input.CharLimit = limit
	offset = min(max(0, offset), len(text))
	if offset == len(text) {
		m.syncComposerViewport()
		return
	}
	wasFocused := m.input.Focused()
	m.input.Focus()
	m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl})
	prefix := text[:offset]
	row := strings.Count(prefix, "\n")
	for m.input.Line() < row {
		m.input.CursorDown()
	}
	column := utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:])
	m.input.SetCursorColumn(column)
	if !wasFocused {
		m.input.Blur()
	}
	m.syncComposerViewport()
}

func (m *Model) receiveTextPaste(text string) {
	if !utf8.ValidString(text) || len(text) > maxDraftBytes {
		m.state.Notice = "Paste exceeds 8 MiB or is not UTF-8 · draft preserved · use /attach for a file"
		return
	}
	text = theme.Clean(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	if text == "" {
		return
	}
	m.syncPastes()
	before := m.input.Value()
	buffer := m.currentDraft()
	start, end := m.composerOffset(before), m.composerOffset(before)
	if a, b, selected := m.input.Selection(); selected {
		start, end = positionOffset(before, a), positionOffset(before, b)
	}
	for _, span := range pasteSpans(before, buffer.Pastes) {
		if start == end && start > span.start && start < span.end {
			start, end = span.end, span.end
		} else if start < span.end && end > span.start {
			start, end = min(start, span.start), max(end, span.end)
		}
	}
	insert := text
	pastes := buffer.Pastes
	large := transcript.LargeText(text)
	if large {
		if len(pastes) >= maxPasteBlocks {
			m.state.Notice = "Too many pasted blocks · nothing pasted · use /editor to consolidate the draft"
			return
		}
		paste := m.newPaste(text, before)
		insert = paste.Marker
		pastes = append(append([]pastedText(nil), pastes...), paste)
	} else {
		// Match normal textarea tab insertion for small, editable pastes.
		insert = strings.ReplaceAll(text, "\t", "    ")
	}
	after := before[:start] + insert + before[end:]
	if expandedBytes(after, pastes) > maxDraftBytes || m.input.CharLimit > 0 && utf8.RuneCountInString(after) > m.input.CharLimit {
		m.state.Notice = "Paste would exceed the draft limit · nothing pasted · draft preserved"
		return
	}
	buffer.Pastes = pastes
	m.setComposerAt(after, start+len(insert))
	buffer.Text, buffer.Loaded = m.input.Value(), true
	m.clearTextSelection()
	m.syncPastes()
	if large {
		m.state.Notice = "Large paste collapsed · " + m.binding("pastes") + " reviews or removes blocks · /editor edits the full draft"
	}
}

// Editing a marker is atomic: deletion removes the whole block, insertion in
// its middle moves after it, and ordinary cursor navigation skips its interior.
func (m *Model) updateComposer(msg tea.Msg) tea.Cmd {
	before := m.input.Value()
	caret := m.composerOffset(before)
	spans := pasteSpans(before, m.currentDraft().Pastes)
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	after := m.input.Value()
	if before == after {
		if !m.input.HasSelection() {
			next := m.composerOffset(after)
			for _, span := range spans {
				if next > span.start && next < span.end {
					if next < caret {
						m.setComposerAt(after, span.start)
					} else {
						m.setComposerAt(after, span.end)
					}
					break
				}
			}
		}
		return cmd
	}
	start := 0
	for start < min(len(before), len(after)) && before[start] == after[start] {
		start++
	}
	suffix := 0
	for suffix < min(len(before)-start, len(after)-start) && before[len(before)-suffix-1] == after[len(after)-suffix-1] {
		suffix++
	}
	end := len(before) - suffix
	insert := after[start : len(after)-suffix]
	left, right := start, end
	removedBlock := false
	for _, span := range spans {
		if start == end && start > span.start && start < span.end {
			left, right = span.end, span.end
		} else if start < span.end && end > span.start {
			removedBlock = true
			left, right = min(left, span.start), max(right, span.end)
		}
	}
	if removedBlock {
		m.clearedDraft = expandPastes(before, m.currentDraft().Pastes)
	}
	if left != start || right != end {
		after = before[:left] + insert + before[right:]
		m.setComposerAt(after, left+len(insert))
	}
	if expandedBytes(after, m.currentDraft().Pastes) > maxDraftBytes {
		m.setComposerAt(before, caret)
		m.state.Notice = "Draft limit reached · edit rejected · pasted text preserved"
	}
	return cmd
}

func (m *Model) syncPastes() {
	buffer := m.currentDraft()
	display := m.input.Value()
	kept := make([]pastedText, 0, len(buffer.Pastes))
	for _, paste := range buffer.Pastes {
		if strings.Contains(display, paste.Marker) {
			kept = append(kept, paste)
		}
	}
	buffer.Pastes = kept
}

func pastePreview(text string, limit int) string {
	if len(text) <= limit {
		return theme.Clean(text)
	}
	for !utf8.RuneStart(text[limit]) {
		limit--
	}
	return theme.Clean(text[:limit]) + "\n\n… preview limited; /editor opens the complete draft after closing this dialog."
}

func (m *Model) openPastes() {
	m.syncPastes()
	var items []overlays.Item
	for _, paste := range m.currentDraft().Pastes {
		items = append(items, overlays.Item{ID: fmt.Sprint(paste.ID), Label: paste.Marker,
			Detail: fmt.Sprintf("%d bytes", len(paste.Text)), Preview: pastePreview(paste.Text, 8192), Value: paste})
	}
	m.openList("pastes", "Pasted text blocks", items)
	m.list.Description = "Full text stays in the draft and is sent in place of its marker."
	m.list.EmptyMessage = "No collapsed text blocks. Large pastes are folded automatically."
	m.list.Hint = "Enter inspect · Del remove · Esc close"
}

func (m *Model) removePaste(paste pastedText) {
	m.clearedDraft = m.draftText()
	display := m.input.Value()
	caret := m.composerOffset(display)
	start := strings.Index(display, paste.Marker)
	if start < 0 {
		return
	}
	shift := 0
	for _, span := range pasteSpans(display, []pastedText{paste}) {
		if caret > span.start {
			shift += min(caret-span.start, span.end-span.start)
		}
	}
	m.setComposerAt(strings.ReplaceAll(display, paste.Marker, ""), caret-shift)
	m.syncPastes()
	if m.creatingSession {
		m.pendingFirstPrompt = nil
	}
	m.stashDraft()
	m.openPastes()
	m.state.Notice = "Pasted block removed · /restore recovers the previous draft into an empty composer"
}
