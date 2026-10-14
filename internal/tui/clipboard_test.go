package tui

import (
	"errors"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
)

func clipboardResult(m *Model) clipboardImageMsg {
	return clipboardImageMsg{attachment: harness.Attachment{Name: "clipboard.png", MIME: "image/png", Image: true, Data: []byte("test image")}, generation: m.clipboardGeneration, conversation: m.state.Active()}
}

func TestClipboardImageStagesWithoutSendingOrChangingDraft(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Peer.Capabilities.Images = true
	m.input.SetValue("Explain this screenshot")
	m.attachments = []harness.Attachment{{Name: "keep.md"}}
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}); cmd == nil || !m.pastingImage {
		t.Fatal("Ctrl+V did not start clipboard read")
	}
	if cmd := m.send(); cmd != nil || m.input.Value() != "Explain this screenshot" || len(m.attachments) != 1 {
		t.Fatal("send raced clipboard read")
	}
	m.Update(clipboardResult(m))
	if m.pastingImage || m.input.Value() != "Explain this screenshot" || len(m.attachments) != 2 || !m.attachments[1].Image || len(adapter.prompts) != 0 {
		t.Fatal("paste lost draft or sent a prompt")
	}
}

func TestClipboardImageFailureFallsBackToTextOnlyWhenRequested(t *testing.T) {
	m := testModel(t)
	m.state.Connection = "ready"
	msg := clipboardResult(m)
	msg.err, msg.fallback = app.ErrNoClipboardImage, true
	if cmd := m.receiveClipboardImage(msg); cmd == nil || len(m.attachments) != 0 {
		t.Fatal("ordinary text paste did not fall back")
	}
	msg.generation, msg.fallback = m.clipboardGeneration, false
	if cmd := m.receiveClipboardImage(msg); cmd != nil || !errors.Is(m.state.LastError, app.ErrNoClipboardImage) {
		t.Fatal("explicit image paste did not report missing image")
	}
	msg.fallback = true
	m.focusedTranscript = true
	if cmd := m.receiveClipboardImage(msg); cmd != nil {
		t.Fatal("late text fallback reached transcript focus")
	}
}

func TestClipboardImageCapabilityAndStaleResults(t *testing.T) {
	m := testModel(t)
	m.state.Connection = "ready"
	if cmd := m.pasteClipboardImage(false); cmd != nil || m.pastingImage {
		t.Fatal("unsupported harness read clipboard")
	}
	if !strings.Contains(m.state.Notice, "does not support") {
		t.Fatal("missing capability warning")
	}
	m.state.Peer.Capabilities.Images = true
	m.pasteClipboardImage(false)
	msg := clipboardResult(m)
	m.perform("clear_attachments")
	m.receiveClipboardImage(msg)
	if len(m.attachments) != 0 || m.pastingImage {
		t.Fatal("detached pending image was reattached")
	}
	m.pasteClipboardImage(false)
	msg = clipboardResult(m)
	msg.conversation = &app.Conversation{}
	m.receiveClipboardImage(msg)
	if len(m.attachments) != 0 || m.pastingImage {
		t.Fatal("clipboard result leaked across conversations")
	}
	m.pasteClipboardImage(false)
	msg = clipboardResult(m)
	m.state.Peer.Capabilities.Images = false
	m.receiveClipboardImage(msg)
	if len(m.attachments) != 0 {
		t.Fatal("capability change accepted image")
	}
}

func TestPasteImageSlashCommandAndRemap(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Peer.Capabilities.Images = true
	m.input.SetValue("/paste-image")
	if cmd := m.send(); cmd == nil || !m.pastingImage || len(adapter.prompts) != 0 {
		t.Fatal("image paste slash command was not local")
	}
	m.opts.Settings.Keybindings = map[string]string{"paste": "alt+v"}
	if m.actionFor("alt+v") != "paste" || m.actionFor("ctrl+v") != "" {
		t.Fatal("paste remapping ignored")
	}
}

func TestClipboardImageCanBeSentWithoutText(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Peer.Capabilities.Images = true
	m.pasteClipboardImage(false)
	m.receiveClipboardImage(clipboardResult(m))
	cmd := m.send()
	if cmd == nil {
		t.Fatal("image-only prompt could not be sent")
	}
	cmd()
	if len(adapter.prompts) != 1 || adapter.prompts[0].Text != "" || len(adapter.prompts[0].Attachments) != 1 || !adapter.prompts[0].Attachments[0].Image || string(adapter.prompts[0].Attachments[0].Data) != "test image" {
		t.Fatal("clipboard image was not included in the prompt")
	}
}
