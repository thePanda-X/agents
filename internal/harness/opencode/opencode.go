// Package opencode registers OpenCode's native ACP command. All protocol
// behavior is provided by the shared adapter, not duplicated here.
package opencode

import (
	"agents/internal/acpclient"
	"agents/internal/harness"
)

func Descriptor() harness.Descriptor {
	return harness.Descriptor{ID: "opencode", Name: "OpenCode", Description: "OpenCode V2 · native ACP v1", Launch: harness.Launch{Command: "opencode", Args: []string{"acp"}}, LoginHint: "Sign in with `opencode auth login`, then reconnect.", Factory: acpclient.New}
}
