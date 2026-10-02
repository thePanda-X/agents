package acpclient

import (
	"encoding/json"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func TestOpenCodeStyleThoughtChunksSurviveDecodeAndReduction(t *testing.T) {
	state := app.NewState()
	state.Apply(harness.Event{Kind: harness.SessionReady, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "session"}}})
	for _, chunk := range []struct{ kind, text string }{
		{"agent_thought_chunk", ""},
		{"agent_thought_chunk", "Checking "},
		{"agent_message_chunk", "Visible response"},
		{"agent_thought_chunk", "the implementation."},
	} {
		data, err := json.Marshal(map[string]any{"sessionId": "session", "update": map[string]any{"sessionUpdate": chunk.kind, "messageId": "message", "content": map[string]any{"type": "text", "text": chunk.text}}})
		if err != nil {
			t.Fatal(err)
		}
		var notification acp.SessionNotification
		if err := json.Unmarshal(data, &notification); err != nil {
			t.Fatal(err)
		}
		event, err := normalizeUpdate(&notification)
		if err != nil {
			t.Fatal(err)
		}
		state.Apply(event)
	}
	entries := state.Active().Entries
	if len(entries) != 2 || entries[0].Role != "thought" || entries[0].Text != "Checking the implementation." || entries[1].Text != "Visible response" {
		t.Fatalf("reasoning was lost or merged into assistant response: %+v", entries)
	}
}
