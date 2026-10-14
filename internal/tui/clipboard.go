package tui

import (
	"errors"
	"time"

	"agents/internal/app"
	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) copyText(text, label string) tea.Cmd {
	// OSC52 writes have no acknowledgement: report dispatch, not verified OS
	// clipboard access. Refresh the toast even when the same text is copied twice.
	m.state.Notice = "✓ " + label + " copy requested · terminal clipboard required"
	m.noticeAt = time.Now()
	return tea.SetClipboard(text)
}

type clipboardImageMsg struct {
	attachment       harness.Attachment
	err              error
	generation       uint64
	conversation     *app.Conversation
	fallback         bool
	buffer           *draftBuffer
	bufferGeneration uint64
	before, selected string
	caret            int
	draftChecked     bool
}

func (m *Model) pasteClipboardImage(fallback bool) tea.Cmd {
	if m.focusedTranscript {
		m.state.Notice = "Focus the composer before pasting · Tab to compose"
		return nil
	}
	if m.pastingImage || m.pastingText {
		m.state.Notice = "Reading clipboard · please wait"
		return nil
	}
	if !m.state.Peer.Capabilities.Images {
		m.state.Notice = "This agent does not support image prompts"
		if fallback {
			return m.pasteClipboardText()
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
	before, selected := m.input.Value(), m.input.SelectedText()
	caret := m.composerOffset(before)
	return func() tea.Msg {
		attachment, err := app.ReadClipboardImage(ctx)
		return clipboardImageMsg{attachment: attachment, err: err, generation: generation, conversation: c, fallback: fallback,
			buffer: buffer, bufferGeneration: bufferGeneration, before: before, selected: selected, caret: caret, draftChecked: true}
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
			if msg.draftChecked && (m.input.Value() != msg.before || m.composerOffset(msg.before) != msg.caret || m.input.SelectedText() != msg.selected) {
				m.state.Notice = "Text paste cancelled after draft changed · paste again when ready"
				return nil
			}
			if !errors.Is(msg.err, app.ErrNoClipboardImage) {
				m.state.Notice = "Image paste unavailable · trying text paste · " + msg.err.Error()
			} else {
				m.state.Notice = "Text paste requested"
			}
			// Focus may have changed while the clipboard command was running.
			if m.overlay == "" && m.interaction == nil && !m.focusedTranscript {
				return m.pasteClipboardText()
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

type clipboardTextMsg struct {
	text, before, selected       string
	caret                        int
	err                          error
	buffer                       *draftBuffer
	generation, bufferGeneration uint64
}

// Use an owned message rather than textarea's private clipboard message so
// text fallback gets the same folding, limits and stale-result checks as a
// terminal bracketed paste. Clipboard reads never send a prompt.
func (m *Model) pasteClipboardText() tea.Cmd {
	if m.pastingText || m.pastingImage || m.focusedTranscript || m.overlay != "" || m.interaction != nil {
		return nil
	}
	buffer := m.currentDraft()
	buffer.Pending++
	m.pastingText = true
	m.clipboardGeneration++
	result := clipboardTextMsg{buffer: buffer, generation: m.clipboardGeneration,
		bufferGeneration: buffer.Generation, before: m.input.Value(), selected: m.input.SelectedText()}
	result.caret = m.composerOffset(result.before)
	ctx := m.opts.Context
	return func() tea.Msg {
		result.text, result.err = app.ReadClipboardText(ctx)
		return result
	}
}

func (m *Model) receiveClipboardText(msg clipboardTextMsg) {
	if msg.generation == m.clipboardGeneration {
		m.pastingText = false
	}
	if msg.buffer == nil || msg.bufferGeneration != msg.buffer.Generation {
		return
	}
	msg.buffer.Pending = max(0, msg.buffer.Pending-1)
	if msg.generation != m.clipboardGeneration || msg.buffer != m.currentDraft() {
		return
	}
	if msg.err != nil {
		m.state.LastError = msg.err
		return
	}
	if m.overlay != "" || m.interaction != nil || m.focusedTranscript ||
		m.input.Value() != msg.before || m.composerOffset(msg.before) != msg.caret || m.input.SelectedText() != msg.selected {
		m.state.Notice = "Text paste cancelled after focus or draft changed · paste again when ready"
		return
	}
	m.receiveTextPaste(msg.text)
}
