package cli

import (
	"agents/internal/acpclient"
	"agents/internal/harness"
	"agents/internal/harness/claude"
	"agents/internal/harness/demo"
	"agents/internal/harness/opencode"
	"agents/internal/harness/pi"
	"agents/internal/storage"
)

func newRegistry(agents []storage.CustomAgent) (*harness.Registry, error) {
	registry := harness.NewRegistry()
	customIDs := make(map[string]bool, len(agents))
	for _, agent := range agents {
		customIDs[agent.ID] = true
	}
	for _, descriptor := range []harness.Descriptor{opencode.Descriptor(), claude.Descriptor(), pi.Descriptor(), demo.Descriptor()} {
		// Preserve existing launch settings when a formerly custom agent
		// becomes built in. Other built-in IDs still reject duplicates.
		if (descriptor.ID == "claude" || descriptor.ID == "pi") && customIDs[descriptor.ID] {
			continue
		}
		if err := registry.Register(descriptor); err != nil {
			return nil, err
		}
	}
	for _, custom := range agents {
		descriptor := harness.Descriptor{ID: custom.ID, Name: custom.Name, Description: "Custom ACP agent", Factory: acpclient.New}
		switch custom.ID {
		case "claude":
			descriptor = claude.Descriptor()
		case "pi":
			descriptor = pi.Descriptor()
		}
		if custom.ID == "claude" || custom.ID == "pi" {
			descriptor.Name = custom.Name
		}
		descriptor.Launch = harness.Launch{Command: custom.Command, Args: custom.Args, Env: custom.Env}
		if err := registry.Register(descriptor); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
