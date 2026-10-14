package tui

import (
	"fmt"
	"time"

	"agents/internal/harness"
	"agents/internal/storage"

	tea "charm.land/bubbletea/v2"
)

type saveMsg struct {
	revision uint64
	key      draftKey
	draft    string
	err      error
}

func (m *Model) restoreDraft() {
	c := m.state.Active()
	if c == nil {
		return
	}
	if !c.DraftLoaded {
		persisted := m.opts.Snapshot.Workspaces[storage.Key(c.Info.Workspace, m.opts.Descriptor.ID)]
		c.Draft = persisted.Drafts[c.Info.ID]
		c.DraftLoaded = true
	}
	buffer := m.currentDraft()
	if buffer.Loaded {
		m.setComposerAt(buffer.Text, len(buffer.Text))
	} else {
		m.setDraftText(c.Draft)
	}
	m.lastSavedDraft = m.input.Value()
}

func (m *Model) Snapshot() (storage.Settings, storage.Snapshot) {
	m.stashDraft()
	if c := m.state.Active(); c != nil {
		c.Draft = m.draftText()
		c.DraftLoaded = true
	}
	m.saveRevision++
	snapshot := storage.Snapshot{Version: 1, Revision: m.saveRevision, Workspaces: make(map[string]storage.WorkspaceState)}
	for key, workspace := range m.opts.Snapshot.Workspaces {
		snapshot.Workspaces[key] = workspace
	}
	if !m.opts.Demo {
		for _, c := range m.state.Conversations {
			if c.Info.Workspace == "" {
				continue
			}
			key := storage.Key(c.Info.Workspace, m.opts.Descriptor.ID)
			workspace := snapshot.Workspaces[key]
			drafts := make(map[string]string)
			for id, draft := range workspace.Drafts {
				drafts[id] = draft
			}
			if c.Draft != "" {
				drafts[c.Info.ID] = c.Draft
			} else {
				delete(drafts, c.Info.ID)
			}
			workspace.Drafts = drafts
			if c.Info.ID == m.state.ActiveID {
				workspace.Session = c.Info
			}
			snapshot.Workspaces[key] = workspace
		}
		if m.state.Active() == nil {
			key := storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)
			workspace := snapshot.Workspaces[key]
			drafts := make(map[string]string)
			for id, draft := range workspace.Drafts {
				drafts[id] = draft
			}
			if m.input.Value() == "" {
				delete(drafts, "")
			} else {
				drafts[""] = m.draftText()
			}
			workspace.Drafts = drafts
			snapshot.Workspaces[key] = workspace
		}
	}
	return m.opts.Settings, snapshot
}

func (m *Model) autosave(now time.Time) tea.Cmd {
	if m.savePending != 0 || now.Before(m.saveRetryAt) || now.Sub(m.lastSave) < time.Second {
		return nil
	}
	if m.input.Value() == m.lastSavedDraft && !m.saveRetry {
		return nil
	}
	return m.save()
}

func (m *Model) save() tea.Cmd {
	if m.opts.Store == nil {
		return nil
	}
	settings, snapshot := m.Snapshot()
	m.opts.Snapshot = snapshot
	m.savePending, m.lastSave = snapshot.Revision, time.Now()
	key, draft, store := m.draftKey(), m.input.Value(), m.opts.Store
	return func() tea.Msg {
		return saveMsg{revision: snapshot.Revision, key: key, draft: draft, err: store.Save(settings, snapshot)}
	}
}

func (m *Model) receiveSave(msg saveMsg) {
	// A newer explicit save may finish first. Neither an old success nor an old
	// failure may mark the current draft saved or reset its retry state.
	if msg.revision != m.savePending {
		return
	}
	m.savePending = 0
	if msg.err != nil {
		m.saveFailures++
		m.saveRetry = true
		m.saveRetryAt = time.Now().Add(min(30*time.Second, time.Second<<min(m.saveFailures-1, 5)))
		m.saveError = fmt.Errorf("save client state: %w", msg.err)
		m.state.LastError = m.saveError
		return
	}
	if msg.key == m.draftKey() {
		m.lastSavedDraft = msg.draft
	}
	m.saveFailures, m.saveRetry, m.saveRetryAt = 0, false, time.Time{}
	if m.saveError != nil && m.state.LastError == m.saveError {
		m.state.LastError = nil
	}
	m.saveError = nil
}

func (m *Model) forgetSession(id string) {
	for key, workspace := range m.opts.Snapshot.Workspaces {
		drafts := make(map[string]string)
		for draftID, draft := range workspace.Drafts {
			if draftID != id {
				drafts[draftID] = draft
			}
		}
		workspace.Drafts = drafts
		if workspace.Session.ID == id {
			workspace.Session = harness.SessionInfo{}
		}
		m.opts.Snapshot.Workspaces[key] = workspace
	}
}

func (m *Model) rememberLocalDraft() {
	key := storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)
	workspace := m.opts.Snapshot.Workspaces[key]
	drafts := make(map[string]string)
	for id, draft := range workspace.Drafts {
		drafts[id] = draft
	}
	if m.input.Value() == "" {
		delete(drafts, "")
	} else {
		drafts[""] = m.draftText()
	}
	workspace.Drafts = drafts
	if m.opts.Snapshot.Workspaces == nil {
		m.opts.Snapshot.Workspaces = make(map[string]storage.WorkspaceState)
	}
	m.opts.Snapshot.Workspaces[key] = workspace
}

func (m *Model) clearLocalDraft() {
	if m.opts.Demo {
		return
	}
	key := storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)
	workspace := m.opts.Snapshot.Workspaces[key]
	drafts := make(map[string]string)
	for id, draft := range workspace.Drafts {
		if id != "" {
			drafts[id] = draft
		}
	}
	workspace.Drafts = drafts
	if m.opts.Snapshot.Workspaces == nil {
		m.opts.Snapshot.Workspaces = make(map[string]storage.WorkspaceState)
	}
	m.opts.Snapshot.Workspaces[key] = workspace
}
