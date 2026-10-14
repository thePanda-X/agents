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
	}
}

// New adds an actionable setup hint for the default bridge without substituting
// a native CLI, downloading executables, or changing command overrides.
func New(ctx context.Context, options harness.Options, sink harness.Sink) (harness.Adapter, error) {
	// Bypass may be omitted by the bridge (for example by managed policy).
	// Accept edits is the least restrictive remaining verified mode.
	options.DefaultModes = []string{"bypassPermissions", "acceptEdits"}
	adapter, err := acpclient.New(ctx, options, sink)
	if options.Launch.Command == command && errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%w; install the Claude ACP bridge with `npm install -g @agentclientprotocol/claude-agent-acp` (Node.js 22+), then reconnect", err)
	}
	return adapter, err
}
