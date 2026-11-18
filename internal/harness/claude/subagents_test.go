package claude

import (
	"encoding/json"
	"testing"

	"agents/internal/harness"
)

func TestDecodeParent(t *testing.T) {
	for _, test := range []struct{ name, meta, want string }{
		{"stamped", `{"claudeCode":{"toolName":"Read","parentToolUseId":"toolu_1"}}`, "toolu_1"},
		{"top level", `{"claudeCode":{"toolName":"Read"}}`, ""},
		{"null", `{"claudeCode":{"parentToolUseId":null}}`, ""},
		{"wrong type", `{"claudeCode":{"parentToolUseId":7}}`, ""},
		{"malformed namespace", `{"claudeCode":"x"}`, ""},
		{"other namespace", `{"other":{"parentToolUseId":"toolu_1"}}`, ""},
		{"not json", `{`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := decodeParent(json.RawMessage(test.meta)); got != test.want {
				t.Fatalf("decodeParent(%s) = %q, want %q", test.meta, got, test.want)
			}
		})
	}
}

func TestConfigureRequestsSubagentTranscript(t *testing.T) {
	caller := map[string]any{"other": "kept"}
	options := configure(harness.Options{ClientMeta: caller})
	if options.ClientMeta[subagentTranscriptCapability] != true || options.ClientMeta["other"] != "kept" || options.DecodeParent == nil {
		t.Fatalf("subagent transcript not negotiated: %+v", options.ClientMeta)
	}
	if _, mutated := caller[subagentTranscriptCapability]; mutated {
		t.Fatal("configure mutated the caller's metadata map")
	}
}
