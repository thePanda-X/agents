// Package pi registers Pi through the pi-acp bridge. Protocol behavior stays
// in the shared ACP adapter rather than being duplicated here.
package pi

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"agents/internal/acpclient"
	"agents/internal/harness"
)

const command = "pi-acp"

func Descriptor() harness.Descriptor {
	return harness.Descriptor{
		ID:          "pi",
		Name:        "Pi",
		Description: "Pi · pi-acp bridge",
		Launch:      harness.Launch{Command: command},
		LoginHint:   "Sign in to Pi with `pi-acp --terminal-login`, then reconnect.",
		Factory:     New,
	}
}

// New explains how to install the default bridge without replacing launch
// overrides, downloading executables, or substituting the native Pi CLI.
func New(ctx context.Context, options harness.Options, sink harness.Sink) (harness.Adapter, error) {
	adapter, err := acpclient.New(ctx, options, sink)
	if options.Launch.Command == command && errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%w; install the Pi ACP bridge with `npm install -g pi-acp` (Node.js 22+ and Pi 0.81.0+), then reconnect", err)
	}
	return adapter, err
}
