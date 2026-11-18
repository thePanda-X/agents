package claude

import "encoding/json"

// subagentTranscriptCapability is the client capability, declared in the
// initialize _meta, with which claude-agent-acp forwards a subagent's text and
// thinking on the root session in addition to its tool calls. The bridge
// documents it as the flattened-transcript contract for clients without
// native subagent sessions.
const subagentTranscriptCapability = "subagent-transcript"

// decodeParent reads the Agent/Task tool call that the bridge stamps on every
// update a subagent produces: {"claudeCode": {"parentToolUseId": "..."}}.
// Unknown keys and malformed values mean the update is top level.
func decodeParent(meta json.RawMessage) string {
	var envelope struct {
		ClaudeCode struct {
			ParentToolUseID json.RawMessage `json:"parentToolUseId"`
		} `json:"claudeCode"`
	}
	if json.Unmarshal(meta, &envelope) != nil {
		return ""
	}
	var parent string
	if json.Unmarshal(envelope.ClaudeCode.ParentToolUseID, &parent) != nil {
		return ""
	}
	return parent
}
