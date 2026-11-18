package acpclient

import (
	"context"
	"encoding/json"
	"testing"

	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

// decodeTestParent mirrors a harness decoder reading a nested parent stamp.
func decodeTestParent(raw json.RawMessage) string {
	var meta struct {
		Agent struct {
			Parent string `json:"parent"`
		} `json:"agent"`
	}
	_ = json.Unmarshal(raw, &meta)
	return meta.Agent.Parent
}

func parentMeta(t *testing.T, parent string) acp.Opt[acp.Meta] {
	t.Helper()
	meta, err := acp.NewMeta(map[string]any{"agent": map[string]any{"parent": parent}})
	if err != nil {
		t.Fatal(err)
	}
	return acp.OptValue(meta)
}

func TestNormalizeUpdateAttributesSubagentUpdatesToParent(t *testing.T) {
	decode := decoders{parent: decodeTestParent}
	for name, update := range map[string]acp.SessionUpdate{
		"message":     &acp.AgentMessageChunk{ContentChunk: acp.ContentChunk{Content: &acp.TextContent{Text: "hi"}, Meta: parentMeta(t, "task-1")}},
		"thought":     &acp.AgentThoughtChunk{ContentChunk: acp.ContentChunk{Content: &acp.TextContent{Text: "hm"}, Meta: parentMeta(t, "task-1")}},
		"tool call":   &acp.ToolCall{ToolCallID: "read-1", Title: "Read", Meta: parentMeta(t, "task-1")},
		"tool update": &acp.ToolCallUpdate{ToolCallID: "read-1", Meta: parentMeta(t, "task-1")},
	} {
		t.Run(name, func(t *testing.T) {
			event, err := normalizeUpdate(&acp.SessionNotification{SessionID: "s", Update: update}, decode)
			if err != nil {
				t.Fatal(err)
			}
			if got := event.Chunk.ParentID + event.Tool.ParentID; got != "task-1" {
				t.Fatalf("parent = %q, want task-1: %+v", got, event)
			}
		})
	}
}

func TestNormalizeUpdateIgnoresParentWithoutDecoderOrSelfReference(t *testing.T) {
	update := &acp.ToolCall{ToolCallID: "task-1", Title: "Agent", Meta: parentMeta(t, "task-1")}
	event, err := normalizeUpdate(&acp.SessionNotification{SessionID: "s", Update: update}, decoders{parent: decodeTestParent})
	if err != nil || event.Tool.ParentID != "" {
		t.Fatalf("a launch must not be its own parent: %+v, %v", event.Tool, err)
	}
	chunk := &acp.AgentMessageChunk{ContentChunk: acp.ContentChunk{Content: &acp.TextContent{Text: "hi"}, Meta: parentMeta(t, "task-1")}}
	event, err = normalizeUpdate(&acp.SessionNotification{SessionID: "s", Update: chunk}, decoders{})
	if err != nil || event.Chunk.ParentID != "" {
		t.Fatalf("parent decoded without a harness decoder: %+v, %v", event.Chunk, err)
	}
}

func TestPermissionRequestCarriesSubagentParent(t *testing.T) {
	var got harness.Event
	client := &Client{options: harness.Options{DecodeParent: decodeTestParent}, sink: func(event harness.Event) {
		if event.Kind == harness.InteractionRequested {
			got = event
			event.Interaction.Reply <- harness.Decision{Action: "cancel"}
		}
	}}
	_, err := client.requestPermission(context.Background(), &acp.RequestPermissionRequest{SessionID: "s", ToolCall: acp.ToolCallUpdate{ToolCallID: "bash-1", Meta: parentMeta(t, "task-1")}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Interaction == nil || got.Interaction.Tool.ParentID != "task-1" {
		t.Fatalf("permission lost its subagent parent: %+v", got.Interaction)
	}
}

func TestClientConfigDeclaresHarnessCapabilityMeta(t *testing.T) {
	client := &Client{options: harness.Options{ClientMeta: map[string]any{"example-extension": true}}}
	config, err := client.clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := config.Capabilities.Meta.Get()
	var enabled bool
	if present, err := meta.Decode("example-extension", &enabled); !ok || !present || err != nil || !enabled {
		t.Fatalf("capability meta not declared: ok=%v present=%v enabled=%v err=%v", ok, present, enabled, err)
	}
	config, err = (&Client{}).clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := config.Capabilities.Meta.Get(); ok {
		t.Fatal("portable clients must not declare extension metadata")
	}
}
