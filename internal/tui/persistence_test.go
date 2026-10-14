package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agents/internal/harness"
	"agents/internal/storage"
)

func TestFailedAutosaveRetriesUnchangedDraft(t *testing.T) {
	m := testModel(t)
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m.opts.Store = &storage.Store{Directory: blocked}
	m.input.SetValue("important unsent draft")
	cmd := m.save()
	if m.lastSavedDraft == m.input.Value() {
		t.Fatal("pending write marked the draft saved")
	}
	msg := cmd().(saveMsg)
	if msg.err == nil {
		t.Fatal("fixture did not fail")
	}
	m.Update(msg)
	if !m.saveRetry || m.state.LastError == nil || m.savePending != 0 {
		t.Fatal("failed save did not schedule recovery")
	}
	if m.autosave(m.saveRetryAt.Add(-time.Millisecond)) != nil {
		t.Fatal("autosave ignored retry backoff")
	}
	m.opts.Store.Directory = t.TempDir()
	cmd = m.autosave(m.saveRetryAt.Add(2 * time.Second))
	if cmd == nil {
		t.Fatal("unchanged draft was not retried")
	}
	m.Update(cmd())
	if m.saveRetry || m.lastSavedDraft != m.input.Value() || m.state.LastError != nil {
		t.Fatal("successful retry did not acknowledge the draft")
	}
	_, snapshot, err := m.opts.Store.Load()
	if err != nil || snapshot.Workspaces[storage.Key("/workspace", "demo")].Drafts["s"] != m.input.Value() {
		t.Fatalf("retry did not persist draft: %+v, %v", snapshot, err)
	}
}

func TestSaveCompletionDoesNotAcknowledgeNewerInputOrAnotherConversation(t *testing.T) {
	m := testModel(t)
	m.opts.Store = &storage.Store{Directory: t.TempDir()}
	m.input.SetValue("captured")
	cmd := m.save()
	if m.autosave(time.Now().Add(time.Hour)) != nil {
		t.Fatal("autosave duplicated an in-flight write")
	}
	m.input.SetValue("newer draft")
	m.Update(cmd())
	if m.lastSavedDraft != "captured" || m.input.Value() != "newer draft" {
		t.Fatal("completion marked newer input saved or replaced it")
	}
	cmd = m.save()
	m.state.Apply(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "other", Workspace: "/workspace"}}})
	m.input.SetValue("other draft")
	m.lastSavedDraft = "other saved draft"
	m.Update(cmd())
	if m.lastSavedDraft != "other saved draft" || m.input.Value() != "other draft" {
		t.Fatal("completion acknowledged a different conversation")
	}
}

func TestSaveIgnoresOutOfOrderCompletionsAndBoundsBackoff(t *testing.T) {
	m := testModel(t)
	m.opts.Store = &storage.Store{Directory: t.TempDir()}
	m.input.SetValue("old")
	old := m.save()().(saveMsg)
	m.input.SetValue("new")
	newer := m.save()().(saveMsg)
	m.Update(newer)
	old.err = errors.New("late failure")
	m.Update(old)
	if m.saveRetry || m.lastSavedDraft != "new" || m.state.LastError != nil {
		t.Fatal("stale save failure changed current persistence state")
	}
	for range 10 {
		m.savePending = m.saveRevision + 1
		m.receiveSave(saveMsg{revision: m.savePending, err: errors.New("disk unavailable")})
		if time.Until(m.saveRetryAt) > 30*time.Second {
			t.Fatal("retry backoff exceeded its bound")
		}
	}
}

func TestSuccessfulRetryDoesNotClearUnrelatedError(t *testing.T) {
	m := testModel(t)
	m.opts.Store = &storage.Store{Directory: t.TempDir()}
	m.savePending = 1
	m.receiveSave(saveMsg{revision: 1, err: errors.New("save failed")})
	other := errors.New("unrelated transport error")
	m.state.LastError = other
	cmd := m.save()
	m.Update(cmd())
	if m.state.LastError != other {
		t.Fatal("successful save hid an unrelated error")
	}
}
