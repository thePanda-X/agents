// Package claude registers Claude Code through the Claude Agent SDK ACP bridge.
// Protocol behavior remains in the shared adapter.
package claude

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"agents/internal/acpclient"
	"agents/internal/harness"
)

const command = "claude-agent-acp"

func Descriptor() harness.Descriptor {
	return harness.Descriptor{
		ID:          "claude",
		Name:        "Claude Code",
		Description: "Claude Code · Claude Agent SDK ACP bridge",
		Launch:      harness.Launch{Command: command},
		LoginHint:   "Sign in with `claude-agent-acp --cli auth login`, or set ANTHROPIC_API_KEY in your environment, then reconnect.",
		Factory:     New,
		// Accept edits still asks before commands, so only bypass is flagged.
		PermissiveModes: []string{"bypassPermissions"},
	}
}

// New adds an actionable setup hint for the default bridge without substituting
// a native CLI, downloading executables, or changing command overrides.
func New(ctx context.Context, options harness.Options, sink harness.Sink) (harness.Adapter, error) {
	adapter, err := acpclient.New(ctx, configure(options), sink)
	if options.Launch.Command == command && errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%w; install the Claude ACP bridge with `npm install -g @agentclientprotocol/claude-agent-acp` (Node.js 22+), then reconnect", err)
	}
	return adapter, err
}

// configure applies the bridge-specific behavior verified against
// claude-agent-acp to otherwise portable adapter options.
func configure(options harness.Options) harness.Options {
	// Bypass may be omitted by the bridge (for example by managed policy).
	// Accept edits is the least restrictive remaining verified mode.
	options.DefaultModes = []string{"bypassPermissions", "acceptEdits"}
	options.DecodeUsageLimits = decodeUsageLimits
	// Subagent activity arrives on the root session stamped with the launching
	// Agent/Task call, so the client can follow it nested under that call.
	meta := make(map[string]any, len(options.ClientMeta)+1)
	for key, value := range options.ClientMeta {
		meta[key] = value
	}
	meta[subagentTranscriptCapability] = true
	options.ClientMeta = meta
	options.DecodeParent = decodeParent
	return options
}
