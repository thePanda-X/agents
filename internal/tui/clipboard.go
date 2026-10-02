package tui

import (
	"errors"

	"agents/internal/app"
	"agents/internal/harness"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type clipboardImageMsg struct {
	attachment       harness.Attachment
	err              error
	generation       uint64
	conversation     *app.Conversation
	fallback         bool
	buffer           *draftBuffer
	bufferGeneration uint64
}

func (m *Model) pasteClipboardImage(fallback bool) tea.Cmd {
	if m.focusedTranscript {
		m.state.Notice = "Focus the composer before pasting · Tab to compose"
		return nil
	}
	if m.pastingImage {
		m.state.Notice = "Reading clipboard · please wait"
		return nil
	}
	if !m.state.Peer.Capabilities.Images {
		m.state.Notice = "This agent does not support image prompts"
		if fallback {
			return textarea.Paste
		}
		return nil
	}
	c := m.state.Active()
	m.stashDraft()
	buffer := m.currentDraft()
	buffer.Pending++
	bufferGeneration := buffer.Generation
	m.clipboardGeneration++
	generation := m.clipboardGeneration
	m.pastingImage = true
	m.state.Notice = "Reading clipboard image…"
	ctx := m.opts.Context
	return func() tea.Msg {
		attachment, err := app.ReadClipboardImage(ctx)
		return clipboardImageMsg{attachment: attachment, err: err, generation: generation, conversation: c, fallback: fallback, buffer: buffer, bufferGeneration: bufferGeneration}
	}
}

func (m *Model) receiveClipboardImage(msg clipboardImageMsg) tea.Cmd {
	buffer := msg.buffer
	if buffer == nil {
		if msg.generation != m.clipboardGeneration {
			return nil
		}
		m.pastingImage = false
		buffer = m.currentDraft()
		buffer.Pending = max(0, buffer.Pending-1)
		if msg.conversation != m.state.Active() || m.state.Connection != "ready" || m.sessionPending {
			return nil
		}
	} else {
		if msg.bufferGeneration != buffer.Generation {
			return nil
		}
		buffer.Pending = max(0, buffer.Pending-1)
		if buffer != m.currentDraft() {
			if msg.err == nil {
				buffer.Attachments = append(buffer.Attachments, msg.attachment)
			} else if !errors.Is(msg.err, app.ErrNoClipboardImage) {
				buffer.Error = msg.err
			}
			return nil
		}
		if msg.generation == m.clipboardGeneration {
			m.pastingImage = false
		}
	}
	if msg.err != nil {
		if msg.fallback {
			if !errors.Is(msg.err, app.ErrNoClipboardImage) {
				m.state.Notice = "Image paste unavailable · trying text paste · " + msg.err.Error()
			} else {
				m.state.Notice = "Text paste requested"
			}
			// Focus may have changed while the clipboard command was running.
			if m.overlay == "" && m.interaction == nil && !m.focusedTranscript {
				return textarea.Paste
			}
		} else {
			m.state.LastError = msg.err
		}
		return nil
	}
	if !m.state.Peer.Capabilities.Images {
		m.state.Notice = "This agent does not support image prompts"
		return nil
	}
	m.attachments = append(m.attachments, msg.attachment)
	m.stashDraft()
	m.state.LastError = nil
	m.state.Notice = "Attached " + msg.attachment.Name + " · Enter to send · /detach to remove"
	return nil
}
