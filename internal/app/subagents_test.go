package app

import (
	"testing"

	"agents/internal/harness"
)

func str(s string) *string { return &s }

func tool(s *State, patch harness.ToolPatch) {
	s.Apply(harness.Event{Kind: harness.Tool, Generation: 1, SessionID: "s", Tool: patch})
}

func chunk(s *State, c harness.MessageChunk) {
	s.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: c})
}

func TestSubagentWorkNestsUnderItsLaunch(t *testing.T) {
	s := stateWithSession()
	tool(s, harness.ToolPatch{ID: "task", Title: str("Explore repo"), Status: str("in_progress")})
	before := s.Active().Entries[0].Revision
	chunk(s, harness.MessageChunk{Role: "thought", Text: "Looking", ParentID: "task"})
	tool(s, harness.ToolPatch{ID: "read", Title: str("Read a.go"), Status: str("in_progress"), ParentID: "task"})
	chunk(s, harness.MessageChunk{Role: "assistant", Text: "Found ", ParentID: "task"})
	chunk(s, harness.MessageChunk{Role: "assistant", Text: "it", ParentID: "task"})
	// A later patch without the stamp still updates the nested call in place.
	tool(s, harness.ToolPatch{ID: "read", Status: str("completed")})
	chunk(s, harness.MessageChunk{Role: "assistant", Text: "Main reply"})

	entries := s.Active().Entries
	if len(entries) != 2 || entries[1].Text != "Main reply" {
		t.Fatalf("subagent work leaked into the transcript: %+v", entries)
	}
	launch := entries[0]
	if len(launch.Children) != 3 || launch.Children[0].Role != "thought" || launch.Children[1].Tool.Status != "completed" || launch.Children[2].Text != "Found it" {
		t.Fatalf("unexpected subagent entries: %+v", launch.Children)
	}
	if launch.Revision <= before {
		t.Fatal("nested updates did not advance the launch revision")
	}
	if s.Active().ToolEntry("read") != launch.Children[1] {
		t.Fatal("nested tool not indexed")
	}
}

func TestNestedSubagentsAndPlaceholderLaunch(t *testing.T) {
	s := stateWithSession()
	// The child's update arrives before its launch is reported.
	tool(s, harness.ToolPatch{ID: "grep", Title: str("Grep"), ParentID: "inner"})
	tool(s, harness.ToolPatch{ID: "outer", Title: str("Plan work"), Status: str("in_progress")})
	tool(s, harness.ToolPatch{ID: "inner", Title: str("Research"), Status: str("in_progress"), ParentID: "outer"})
	entries := s.Active().Entries
	if len(entries) != 2 {
		t.Fatalf("expected placeholder and outer launch, got %+v", entries)
	}
	inner := entries[0]
	if inner.ID != "inner" || inner.Tool.Title != "Research" || len(inner.Children) != 1 {
		t.Fatalf("placeholder was not filled in by its launch: %+v", inner.Tool)
	}
	// A launch keeps the place it first appeared; later stamps cannot move it.
	if len(entries[1].Children) != 0 {
		t.Fatal("existing entry was reparented")
	}
	tool(s, harness.ToolPatch{ID: "deep", ParentID: "grep"})
	before := inner.Revision
	tool(s, harness.ToolPatch{ID: "deep", Status: str("completed")})
	if inner.Revision <= before {
		t.Fatal("grandchild change did not reach its ancestors")
	}
}

func TestSubagentPromptDoesNotReconcileUserPrompt(t *testing.T) {
	s := stateWithSession()
	s.Apply(harness.Event{Kind: harness.TurnStarted, Generation: 1, SessionID: "s", Text: "fix it"})
	tool(s, harness.ToolPatch{ID: "task", Title: str("Agent")})
	chunk(s, harness.MessageChunk{Role: "user", Text: "fix it", ParentID: "task"})
	chunk(s, harness.MessageChunk{Role: "user", Text: "fix it"})
	entries := s.Active().Entries
	if len(entries) != 2 || entries[0].Text != "fix it" || entries[0].Role != "user" || len(entries[1].Children) != 1 {
		t.Fatalf("subagent prompt replaced or duplicated the user's prompt: %+v", entries)
	}
}

func TestCancelledTurnStopsSubagentTools(t *testing.T) {
	s := stateWithSession()
	tool(s, harness.ToolPatch{ID: "task", Status: str("in_progress")})
	tool(s, harness.ToolPatch{ID: "bash", Status: str("in_progress"), ParentID: "task"})
	s.Apply(harness.Event{Kind: harness.TurnFinished, Generation: 1, SessionID: "s", Reason: "cancelled"})
	if got := s.Active().ToolEntry("bash").Tool.Status; got != "cancelled" {
		t.Fatalf("nested tool status = %q", got)
	}
}

func TestSubagentPermissionNestsTool(t *testing.T) {
	s := stateWithSession()
	tool(s, harness.ToolPatch{ID: "task"})
	s.Apply(harness.Event{Kind: harness.InteractionRequested, Generation: 1, SessionID: "s", Interaction: &harness.Interaction{Kind: "permission", Tool: harness.ToolPatch{ID: "bash", ParentID: "task"}}})
	if len(s.Active().Entries) != 1 || len(s.Active().Entries[0].Children) != 1 {
		t.Fatalf("permission tool not nested: %+v", s.Active().Entries)
	}
}
