package app

import (
	"context"
	"testing"
	"time"

	"agents/internal/harness"
)

func TestSessionDatesSurviveMetadataReplayAndReady(t *testing.T) {
	s := stateWithSession()
	const old = "2026-10-01T10:00:00Z"
	const updated = "2026-10-12T23:45:00.123456789-07:00"
	s.Active().Info.UpdatedAt = old
	s.Apply(harness.Event{Kind: harness.SessionsListed, Generation: 1, Sessions: []harness.SessionInfo{s.Active().Info}})
	s.Apply(harness.Event{Kind: harness.SessionLoading, Generation: 1, SessionID: "s"})
	s.Apply(harness.Event{Kind: harness.Metadata, Generation: 1, SessionID: "s", Metadata: harness.SessionInfoPatch{UpdatedAt: new(updated)}})
	s.Apply(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: "/project", UpdatedAt: old}}})
	if s.Active().Info.UpdatedAt != updated || s.Sessions[0].UpdatedAt != updated {
		t.Fatal("ready overwrote the reported replay date")
	}
	s.Apply(harness.Event{Kind: harness.Metadata, Generation: 1, SessionID: "s", Metadata: harness.SessionInfoPatch{Title: new("New title")}})
	if s.Active().Info.UpdatedAt != updated {
		t.Fatal("title-only update erased the date")
	}
	s.Apply(harness.Event{Kind: harness.Metadata, Generation: 1, SessionID: "s", Metadata: harness.SessionInfoPatch{UpdatedAt: new("")}})
	if s.Active().Info.UpdatedAt != "" || s.Sessions[0].UpdatedAt != "" {
		t.Fatal("explicitly cleared date was retained")
	}
}

func TestListingRefreshesPersistedConversationDate(t *testing.T) {
	s := stateWithSession()
	const updated = "2026-10-13T12:00:00Z"
	s.Apply(harness.Event{Kind: harness.SessionsListed, Generation: 1, Sessions: []harness.SessionInfo{{ID: "s", UpdatedAt: updated}}})
	if s.Active().Info.UpdatedAt != updated {
		t.Fatal("conversation kept stale metadata after listing")
	}
}

func TestReplayDoesNotAssignResumeTimeToHistoricalEntries(t *testing.T) {
	controller := NewController(context.Background())
	defer controller.Close()
	s := stateWithSession()
	s.Apply(harness.Event{Kind: harness.SessionLoading, Generation: 1, SessionID: "s"})
	for _, e := range []harness.Event{
		{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{Role: "user", Text: "Old prompt"}},
		{Kind: harness.Tool, Generation: 1, SessionID: "s", Tool: harness.ToolPatch{ID: "old-tool"}},
	} {
		controller.publish(e)
		s.Apply(<-controller.Events)
	}
	for _, entry := range s.Active().Entries {
		if !entry.At.IsZero() {
			t.Fatal("replay was stamped with the resume time")
		}
	}
	// If a future adapter supplies an original timestamp, keep it exactly.
	original := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	s.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", At: original, Chunk: harness.MessageChunk{ID: "dated", Role: "assistant", Text: "Dated response"}})
	if !s.Active().Entries[2].At.Equal(original) {
		t.Fatal("original replay timestamp was replaced")
	}
	s.Apply(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s"}}})
	controller.publish(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{ID: "live", Role: "user", Text: "New prompt"}})
	s.Apply(<-controller.Events)
	if s.Active().Entries[3].At.IsZero() {
		t.Fatal("live entries lost their receipt time")
	}
}
