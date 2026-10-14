// Package opencode registers OpenCode's native ACP command. All protocol
// behavior is provided by the shared adapter, not duplicated here.
package opencode

import (
	"context"
	"maps"
	"os"

	"agents/internal/acpclient"
	"agents/internal/harness"
)

func Descriptor() harness.Descriptor {
	return harness.Descriptor{ID: "opencode", Name: "OpenCode", Description: "OpenCode V2 · native ACP v1", Launch: harness.Launch{Command: "opencode", Args: []string{"acp"}}, LoginHint: "Sign in with `opencode auth login`, then reconnect.", Factory: New}
}

const configEnv = "OPENCODE_CONFIG_CONTENT"

// V2's last matching permission rule wins. Include a build-specific rule because
// agent permissions follow global rules. Other modes retain their native policy.
const permissiveConfig = `{"default_agent":"build","permissions":[{"action":"*","resource":"*","effect":"allow"}],"agents":{"build":{"permissions":[{"action":"*","resource":"*","effect":"allow"}]}}}`

func New(ctx context.Context, options harness.Options, sink harness.Sink) (harness.Adapter, error) {
	options.DefaultModes = []string{"build"}
	options.Launch = defaultLaunch(options.Launch)
	return acpclient.New(ctx, options, sink)
}

func defaultLaunch(launch harness.Launch) harness.Launch {
	// Explicit inline configuration is an override, not disposable client state.
	// Preserve JSONC, provider settings, plugins, and any user-selected policy.
	if _, explicit := launch.Env[configEnv]; explicit || os.Getenv(configEnv) != "" {
		return launch
	}
	launch.Env = maps.Clone(launch.Env)
	if launch.Env == nil {
		launch.Env = make(map[string]string)
	}
	launch.Env[configEnv] = permissiveConfig
	return launch
}
