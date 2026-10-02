package app

import (
	"testing"

	"agents/internal/harness"
)

func stateWithSession() *State {
	s := NewState()
	s.Apply(harness.Event{Kind: harness.Connecting, Generation: 1})
	s.Apply(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: "/project"}}})
	return s
}

func TestStaleGenerationCannotMutateState(t *testing.T) {
	s := stateWithSession()
	if s.Apply(harness.Event{Kind: harness.Message, Generation: 0, SessionID: "s", Chunk: harness.MessageChunk{Role: "assistant", Text: "stale"}}) {
		t.Fatal("stale event accepted")
	}
	if len(s.Active().Entries) != 0 {
		t.Fatal("stale event changed transcript")
	}
}

func TestMessageChunksAndOptimisticUserEcho(t *testing.T) {
	for _, ids := range []bool{false, true} {
		t.Run(map[bool]string{false: "without IDs", true: "with IDs"}[ids], func(t *testing.T) {
			s := stateWithSession()
			s.Apply(harness.Event{Kind: harness.TurnStarted, Generation: 1, SessionID: "s", Text: "hello world"})
			id := ""
			if ids {
				id = "user-1"
			}
			for _, text := range []string{"hello ", "world"} {
				s.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{ID: id, Role: "user", Text: text}})
			}
			if len(s.Active().Entries) != 1 || s.Active().Entries[0].Text != "hello world" {
				t.Fatalf("duplicated user echo: %+v", s.Active().Entries)
			}
			for _, text := range []string{"response ", "body"} {
				s.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{Role: "assistant", Text: text}})
			}
			if len(s.Active().Entries) != 2 || s.Active().Entries[1].Text != "response body" {
				t.Fatal("assistant chunks not merged")
			}
		})
	}
}

func TestToolPatchesPreserveUnchangedFields(t *testing.T) {
	s := stateWithSession()
	title, status, text := "Read file", "in_progress", "contents"
	s.Apply(harness.Event{Kind: harness.Tool, Generation: 1, SessionID: "s", Tool: harness.ToolPatch{ID: "tool", Title: &title, Status: &status, Text: &text}})
	status = "completed"
	s.Apply(harness.Event{Kind: harness.Tool, Generation: 1, SessionID: "s", Tool: harness.ToolPatch{ID: "tool", Status: &status}})
	tool := s.Active().Entries[0].Tool
	if tool.Title != title || tool.Text != text || tool.Status != "completed" {
		t.Fatalf("partial patch erased fields: %+v", tool)
	}
}

func TestReplayResetsTranscriptAndPreservesDraft(t *testing.T) {
	s := stateWithSession()
	s.Active().Draft = "keep this"
	s.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{Role: "assistant", Text: "old"}})
	s.Apply(harness.Event{Kind: harness.SessionLoading, Generation: 1, SessionID: "s"})
	s.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{Role: "assistant", Text: "replayed"}})
	if len(s.Active().Entries) != 1 || s.Active().Entries[0].Text != "replayed" || s.Active().Draft != "keep this" {
		t.Fatal("replay duplicated history or discarded draft")
	}
}

func TestConfigUpdatesReplaceEntireOptionSet(t *testing.T) {
	s := stateWithSession()
	s.Active().Options = []harness.ConfigOption{{ID: "model"}, {ID: "effort"}}
	s.Apply(harness.Event{Kind: harness.Config, Generation: 1, SessionID: "s", Options: []harness.ConfigOption{{ID: "model"}}})
	if len(s.Active().Options) != 1 {
		t.Fatal("dependent option survived replacement")
	}
}

func TestDisconnectResolvesPresentationState(t *testing.T) {
	s := stateWithSession()
	s.Active().Busy, s.Active().Cancelling = true, true
	s.Interactions = []*harness.Interaction{{ID: "pending"}}
	s.Apply(harness.Event{Kind: harness.Disconnected, Generation: 1})
	if s.Active().Busy || s.Active().Cancelling || len(s.Interactions) > 0 {
		t.Fatal("disconnect left pending state")
	}
}

func TestReconnectClearsBusyStateEvenWithoutOldDisconnectEvent(t *testing.T) {
	s := stateWithSession()
	s.Active().Busy, s.Active().Cancelling = true, true
	s.Apply(harness.Event{Kind: harness.Connecting, Generation: 2})
	if s.Active().Busy || s.Active().Cancelling {
		t.Fatal("reconnected conversation remains busy")
	}
}
