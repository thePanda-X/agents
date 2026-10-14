package tui

import tea "charm.land/bubbletea/v2"

func (m *Model) toggleCompact() tea.Cmd {
	m.clearTextSelection()
	if m.dirty {
		m.refresh()
	}
	follow := m.viewport.AtBottom()
	anchor, offset := 0, 0
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
	readingOffset := 0
	if m.selected < len(m.positions) {
		readingOffset = max(0, m.browseLine-m.positions[m.selected])
	}
	m.opts.Settings.Compact = !m.opts.Settings.Compact
	m.resize()
	m.refresh()
	// Preserve the visible entry rather than a raw row offset: padding and
	// compact tool groups change the number of rows preceding that entry.
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
	if m.selected < len(m.positions) {
		start := m.positions[m.selected]
		if m.selected+1 < len(m.positions) {
			readingOffset = min(readingOffset, max(0, m.positions[m.selected+1]-start-1))
		}
		m.browseLine = start + readingOffset
	}
	m.dirty = false
	if m.opts.Settings.Compact {
		m.state.Notice = "Compact layout"
	} else {
		m.state.Notice = "Comfortable layout"
	}
	return m.save()
}
