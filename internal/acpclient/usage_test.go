package acpclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func TestUsageUpdatePassesMetadataToOptionalDecoder(t *testing.T) {
	meta, err := acp.NewMeta(map[string]any{
		"_claude/rateLimit": map[string]any{
			"status":      "allowed",
			"utilization": 0.5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded json.RawMessage
	event, err := normalizeUpdate(&acp.SessionNotification{
		SessionID: "s",
		Update: &acp.UsageUpdate{
			Used: 10,
			Size: 100,
			Meta: acp.OptValue(meta),
		},
	}, decoders{usageLimits: func(raw json.RawMessage) []harness.UsageLimit {
		decoded = raw
		return []harness.UsageLimit{{Kind: "five_hour", Label: "5h", Utilization: 0.5}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != harness.UsageChanged || event.Usage.Used != 10 || event.Usage.Size != 100 {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	if len(event.Usage.Limits) != 1 || event.Usage.Limits[0].Kind != "five_hour" {
		t.Fatalf("missing decoded limits: %+v", event.Usage.Limits)
	}
	if !strings.Contains(string(decoded), "_claude/rateLimit") {
		t.Fatalf("decoder did not receive metadata: %s", decoded)
	}
}

func TestClientSessionUpdateUsesConfiguredUsageDecoder(t *testing.T) {
	meta, err := acp.NewMeta(map[string]any{"_claude/rateLimit": map[string]any{"status": "allowed"}})
	if err != nil {
		t.Fatal(err)
	}
	var got harness.Event
	client := &Client{
		options: harness.Options{DecodeUsageLimits: func(json.RawMessage) []harness.UsageLimit {
			return []harness.UsageLimit{{Kind: "five_hour", Label: "5h"}}
		}},
		sink: func(event harness.Event) { got = event },
	}
	config, err := client.clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.SessionUpdate(context.Background(), &acp.SessionNotification{
		SessionID: "s",
		Update:    &acp.UsageUpdate{Used: 1, Size: 2, Meta: acp.OptValue(meta)},
	})
	if got.Kind != harness.UsageChanged || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].Kind != "five_hour" {
		t.Fatalf("session update did not apply the configured decoder: %+v", got)
	}
}

func TestUsageUpdateWithoutMetadataDoesNotCallDecoder(t *testing.T) {
	called := false
	event, err := normalizeUpdate(&acp.SessionNotification{
		SessionID: "s",
		Update:    &acp.UsageUpdate{Used: 10, Size: 100},
	}, decoders{usageLimits: func(json.RawMessage) []harness.UsageLimit {
		called = true
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if called || len(event.Usage.Limits) != 0 {
		t.Fatalf("decoder called without metadata: called=%v limits=%+v", called, event.Usage.Limits)
	}
}
