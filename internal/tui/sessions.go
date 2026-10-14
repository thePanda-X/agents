package tui

import (
	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) connect(restore *harness.SessionInfo) tea.Cmd {
	controller, descriptor, options := m.opts.Controller, m.opts.Descriptor, m.opts.Agent
	m.sessionPending = restore != nil
	m.creatingSession, m.pendingFirstPrompt = false, nil
	m.pendingFirstDraft = ""
	m.effortPending = false
	m.pendingSetup = nil
	m.configPending = false
	return command(func() { controller.Connect(descriptor, options, restore) })
}

func (m *Model) reconnect() tea.Cmd {
	m.stashDraft()
	m.finishSubmission(true)
	var restore *harness.SessionInfo
	if c := m.state.Active(); c != nil {
		c.Draft = m.draftText()
		c.DraftLoaded = true
		info := c.Info
		restore = &info
	}
	return m.connect(restore)
}

func (m *Model) newSession() tea.Cmd {
	if m.sessionPending {
		return nil
	}
	if c := m.state.Active(); c != nil {
		if c.Busy {
			m.state.Notice = "Cancel the active turn before opening another session."
			return nil
		}
		c.Draft = m.draftText()
		c.DraftLoaded = true
		m.stashDraft()
	} else {
		m.state.Notice = "New conversation · send a prompt to create the session"
		return nil
	}
	m.state.ActiveID, m.displayedSessionID = "", ""
	m.input.Reset()
	m.attachments, m.history, m.positions = nil, nil, nil
	m.clipboardGeneration++
	m.pastingImage, m.pastingText = false, false
	m.restoreLocalBuffer()
	m.historyIndex, m.selected, m.browseLine = 0, 0, 0
	m.browseVisited = false
	m.searchQuery, m.browsePrefix = "", ""
	m.dirty = true
	m.state.Notice = "New conversation · send a prompt to create the session"
	return m.setTranscriptFocus(false)
}
