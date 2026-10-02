package tui

import (
	"errors"
	"testing"

	"agents/internal/harness"
	"agents/internal/storage"

	tea "charm.land/bubbletea/v2"
)

func TestNewConversationIsLocalUntilFirstPrompt(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Peer.Capabilities.Images = true
	m.input.SetValue("keep original draft")
	m.newSession()
	if adapter.newSessions != 0 || m.state.Active() != nil || m.sessionPending || m.state.Conversations["s"].Draft != "keep original draft" {
		t.Fatal("new conversation created a harness session or lost original draft")
	}
	if m.send() != nil || adapter.newSessions != 0 {
		t.Fatal("empty send created a session")
	}
	m.input.SetValue("first prompt")
	m.attachments = []harness.Attachment{{Name: "clipboard.png", Image: true, MIME: "image/png", Data: []byte("image")}}
	cmd := m.send()
	if cmd == nil || !m.creatingSession || !m.sessionPending || m.input.Value() != "first prompt" || len(m.attachments) != 1 {
		t.Fatal("first send did not stage lazy creation safely")
	}
	if m.send() != nil {
		t.Fatal("repeated Enter created another session")
	}
	cmd()
	if adapter.newSessions != 1 || len(adapter.prompts) != 0 {
		t.Fatal("prompt sent before session creation completed")
	}
	event := <-m.opts.Controller.Events
	_, followup := m.Update(eventMsg(event))
	if m.state.Active() == nil || m.creatingSession || m.sessionPending || m.input.Value() != "" || len(m.attachments) != 0 {
		t.Fatal("created session did not accept queued first prompt")
	}
	batch, ok := followup().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatal("missing queued prompt command")
	}
	batch[0]() // submit the prompt, not the event-listener command
	if len(adapter.prompts) != 1 || adapter.prompts[0].Text != "first prompt" || len(adapter.prompts[0].Attachments) != 1 {
		t.Fatal("first prompt/attachment lost or sent more than once")
	}
}

func TestLazyCreationFailureAndCancellationPreserveDraft(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		m, adapter := modelWithCommandAdapter(t)
		m.newSession()
		m.input.SetValue("keep me")
		m.attachments = []harness.Attachment{{Name: "keep.md"}}
		create := m.send()
		if cancel {
			m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			if m.pendingFirstPrompt != nil || m.input.Value() != "keep me" {
				t.Fatal("cancel lost draft or retained queued send")
			}
			create()
			m.Update(eventMsg(<-m.opts.Controller.Events))
		} else {
			m.Update(eventMsg(harness.Event{Kind: harness.Failure, Generation: m.state.Generation, Err: errors.New("create failed")}))
		}
		if m.creatingSession || m.sessionPending || m.pendingFirstPrompt != nil || len(adapter.prompts) != 0 || m.input.Value() != "keep me" || len(m.attachments) != 1 {
			t.Fatal("failed/cancelled first send discarded draft or sent prompt")
		}
	}
}

func TestTypingDuringLazyCreationDoesNotChangeSentPromptOrLoseNewDraft(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.newSession()
	m.input.SetValue("original")
	create := m.send()
	m.input.SetValue("edited while creating")
	create()
	_, cmd := m.Update(eventMsg(<-m.opts.Controller.Events))
	cmd().(tea.BatchMsg)[0]()
	if m.input.Value() != "edited while creating" || len(adapter.prompts) != 1 || adapter.prompts[0].Text != "original" {
		t.Fatal("queued send changed or edited draft was lost")
	}
}

func TestLocalDraftPersistsWithoutClaimingNativeSession(t *testing.T) {
	m, _ := modelWithCommandAdapter(t)
	m.newSession()
	m.input.SetValue("unsent local draft")
	_, snapshot := m.Snapshot()
	key := storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)
	if snapshot.Workspaces[key].Drafts[""] != "unsent local draft" {
		t.Fatal("local draft not saved")
	}
	if snapshot.Workspaces[key].Session.ID != "" {
		t.Fatal("local draft invented a native session")
	}
	opts := m.opts
	opts.Snapshot = snapshot
	reopened := New(opts)
	if reopened.state.Active() != nil || reopened.input.Value() != "unsent local draft" {
		t.Fatal("startup did not restore local draft without creating a conversation")
	}
	reopened.state.Peer.Capabilities.Images = true
	reopened.state.Connection = "ready"
	reopened.pasteClipboardImage(false)
	reopened.receiveClipboardImage(clipboardResult(reopened))
	if len(reopened.attachments) != 1 {
		t.Fatal("image paste required a native session")
	}
}

func TestAttachmentOnlyFirstPromptCreatesSessionAndLocalCommandsDoNot(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Peer.Capabilities.Images = true
	m.newSession()
	m.input.SetValue("/settings")
	m.send()
	m.closeOverlay()
	if adapter.newSessions != 0 || m.state.Active() != nil || m.sessionPending {
		t.Fatal("local command created a native session")
	}
	m.attachments = []harness.Attachment{{Name: "image.png", Image: true, Data: []byte("image"), MIME: "image/png"}}
	create := m.send()
	if create == nil {
		t.Fatal("attachment-only first prompt did not create a session")
	}
	create()
	_, cmd := m.Update(eventMsg(<-m.opts.Controller.Events))
	cmd().(tea.BatchMsg)[0]()
	if len(adapter.prompts) != 1 || adapter.prompts[0].Text != "" || len(adapter.prompts[0].Attachments) != 1 {
		t.Fatal("attachment-only first prompt was lost")
	}
}

func TestLazyCreationIgnoresStaleReadyAndReconnectDoesNotCreate(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.newSession()
	m.input.SetValue("draft")
	m.send()
	m.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: m.state.Generation - 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "stale"}}}))
	if m.state.Active() != nil || !m.creatingSession || m.pendingFirstPrompt == nil {
		t.Fatal("stale ready consumed the first prompt")
	}
	cmd := m.reconnect()
	if cmd == nil {
		t.Fatal("missing reconnect command")
	}
	cmd()
	if adapter.newSessions != 0 || m.creatingSession || m.sessionPending || m.input.Value() != "draft" {
		t.Fatal("reconnecting a local draft created a session or lost its text")
	}
}
