package tui

import (
	"fmt"
	"path/filepath"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/storage"
	"agents/internal/tui/overlays"

	tea "charm.land/bubbletea/v2"
)

type draftKey struct{ Harness, Workspace, Session string }

// Attachment bytes and recovery snapshots stay in memory. Each buffer belongs
// to one conversation, including the local draft before session creation.
type draftBuffer struct {
	Text        string
	Loaded      bool
	Pastes      []pastedText
	Attachments []harness.Attachment
	Generation  uint64
	Pending     int
	Submission  *submissionSnapshot
	Failed      *submissionSnapshot
	Error       error
}

type submissionSnapshot struct {
	Draft  string
	Prompt harness.Prompt
}

func (m *Model) draftKey() draftKey {
	key := draftKey{Harness: m.opts.Descriptor.ID, Workspace: m.opts.Agent.Workspace}
	if c := m.state.Active(); c != nil {
		key.Workspace, key.Session = c.Info.Workspace, c.Info.ID
	}
	return key
}

func (m *Model) currentDraft() *draftBuffer {
	key := m.draftKey()
	if m.drafts == nil {
		m.drafts = make(map[draftKey]*draftBuffer)
	}
	if buffer := m.drafts[key]; buffer != nil {
		return buffer
	}
	buffer := &draftBuffer{}
	m.drafts[key] = buffer
	return buffer
}

func (m *Model) stashDraft() {
	buffer := m.currentDraft()
	buffer.Text = m.input.Value()
	buffer.Loaded = true
	buffer.Attachments = append([]harness.Attachment(nil), m.attachments...)
	if c := m.state.Active(); c != nil {
		c.Draft, c.DraftLoaded = m.draftText(), true
	}
}

func (m *Model) restoreAttachments() {
	buffer := m.currentDraft()
	m.attachments = append([]harness.Attachment(nil), buffer.Attachments...)
	if buffer.Error != nil {
		m.state.LastError = buffer.Error
		buffer.Error = nil
	}
}

func (m *Model) clearAttachments() {
	buffer := m.currentDraft()
	buffer.Generation++
	buffer.Pending, buffer.Attachments = 0, nil
	m.attachments = nil
	m.clipboardGeneration++
	m.pastingImage, m.pastingText = false, false
	if m.creatingSession {
		m.pendingFirstPrompt = nil
		m.state.Notice = "Attachments removed · first send cancelled · draft preserved"
	} else {
		m.state.Notice = "Attachments removed"
	}
}

func (m *Model) attach(path string) tea.Cmd {
	key := m.draftKey()
	if !filepath.IsAbs(path) {
		path = filepath.Join(key.Workspace, path)
	}
	m.stashDraft()
	buffer := m.currentDraft()
	buffer.Pending++
	generation := buffer.Generation
	images := m.state.Peer.Capabilities.Images
	m.state.Notice = "Reading attachment…"
	return func() tea.Msg {
		attachment, err := app.ReadAttachment(path)
		if err == nil && attachment.Image && !images {
			err = fmt.Errorf("this agent does not support image prompts")
		}
		return attachedMsg{attachment: attachment, err: err, buffer: buffer, generation: generation}
	}
}

func (m *Model) receiveAttachment(msg attachedMsg) {
	buffer := msg.buffer
	if buffer == nil {
		buffer = m.currentDraft()
	}
	if msg.generation != buffer.Generation {
		return
	}
	buffer.Pending = max(0, buffer.Pending-1)
	active := buffer == m.currentDraft()
	if msg.err != nil {
		if active {
			m.state.LastError = msg.err
		} else {
			buffer.Error = msg.err
		}
		return
	}
	if active {
		if msg.attachment.Image && !m.state.Peer.Capabilities.Images {
			m.state.LastError = fmt.Errorf("this agent does not support image prompts")
			return
		}
		m.attachments = append(m.attachments, msg.attachment)
		buffer.Attachments = append([]harness.Attachment(nil), m.attachments...)
		m.state.LastError = nil
		m.state.Notice = "Attached " + msg.attachment.Name + " · /attachments to review"
	} else {
		buffer.Attachments = append(buffer.Attachments, msg.attachment)
	}
}

func (m *Model) finishSubmission(failed bool) {
	buffer := m.currentDraft()
	if buffer.Submission == nil {
		return
	}
	if failed {
		buffer.Failed = buffer.Submission
		m.state.Notice = "Submission interrupted · /recover restores it for review; nothing is resent automatically"
	}
	buffer.Submission = nil
}

func (m *Model) recoverSubmission() tea.Cmd {
	buffer := m.currentDraft()
	if buffer.Failed == nil {
		m.state.Notice = "No failed submission to recover in this conversation"
		return nil
	}
	if m.input.Value() != "" || len(m.attachments) > 0 || buffer.Pending > 0 || m.pastingImage || m.pastingText {
		m.state.Notice = "Save or clear the current draft and attachments before using /recover"
		return nil
	}
	if c := m.state.Active(); c != nil && (c.Busy || c.Loading) {
		m.state.Notice = "Wait for the current turn before recovering a submission"
		return nil
	}
	if !m.setDraftText(buffer.Failed.Draft) {
		return nil
	}
	m.attachments = append([]harness.Attachment(nil), buffer.Failed.Prompt.Attachments...)
	buffer.Failed = nil
	m.stashDraft()
	m.state.LastError = nil
	m.state.Notice = "Submission restored · review before sending; the agent may have already processed part of it"
	return m.setTranscriptFocus(false)
}

func (m *Model) openAttachments() {
	var items []overlays.Item
	for index, attachment := range m.attachments {
		preview := attachment.Name + "\n\nType: " + attachment.MIME
		if attachment.Path != "" {
			preview += "\nPath: " + attachment.Path
		}
		if attachment.Image {
			preview += fmt.Sprintf("\nImage: %d bytes", len(attachment.Data))
		}
		items = append(items, overlays.Item{ID: fmt.Sprint(index), Label: attachment.Name, Detail: attachment.MIME, Preview: preview, Value: index})
	}
	m.openList("attachments", "Pending attachments", items)
	m.list.EmptyMessage = "No pending attachments. Use /attach or /paste-image."
	m.list.Hint = "Enter inspect · Del remove · Esc close"
	m.list.Description = "Attachments stay with their draft while this client is open."
}

func (m *Model) removeAttachment(index int) {
	if index < 0 || index >= len(m.attachments) {
		return
	}
	name := m.attachments[index].Name
	m.attachments = append(m.attachments[:index:index], m.attachments[index+1:]...)
	m.stashDraft()
	if m.creatingSession {
		m.pendingFirstPrompt = nil
	}
	m.state.Notice = "Removed " + name
	m.openAttachments()
	m.list.Cursor = min(index, max(0, len(m.list.Items)-1))
}

func (m *Model) restoreLocalBuffer() {
	buffer := m.currentDraft()
	text := buffer.Text
	if !buffer.Loaded {
		workspace := m.opts.Snapshot.Workspaces[storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)]
		text = workspace.Drafts[""]
		m.setDraftText(text)
	} else {
		m.setComposerAt(text, len(text))
	}
	m.restoreAttachments()
}

func (m *Model) stashClosedDraft(key draftKey) {
	text := m.input.Value()
	buffer := m.drafts[key]
	if buffer != nil {
		text = expandPastes(text, buffer.Pastes)
	}
	if c := m.state.Conversations[key.Session]; c != nil {
		c.Draft, c.DraftLoaded = text, true
	}
	if buffer != nil {
		buffer.Text = m.input.Value()
		buffer.Attachments = append([]harness.Attachment(nil), m.attachments...)
	}
	m.input.Reset()
	m.attachments = nil
	m.restoreLocalBuffer()
	m.dirty = true
}
