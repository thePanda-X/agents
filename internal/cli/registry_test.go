package cli

import (
	"reflect"
	"testing"

	"agents/internal/harness"
	"agents/internal/storage"
)

func TestBuiltInHarnesses(t *testing.T) {
	registry, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opencode", "claude", "pi", "demo"} {
		descriptor, err := registry.Lookup(id)
		if err != nil || descriptor.Factory == nil {
			t.Fatalf("missing built-in harness %q: %v", id, err)
		}
	}
	if len(registry.List()) != 4 {
		t.Fatalf("unexpected registrations: %+v", registry.List())
	}
}

func TestLegacyCustomBuiltInLaunchIsPreserved(t *testing.T) {
	for _, custom := range []storage.CustomAgent{
		{ID: "claude", Name: "My Claude", Command: "/custom/claude-agent-acp", Args: []string{"--custom"}, Env: map[string]string{"CLAUDE_CODE_EXECUTABLE": "/custom/claude"}},
		{ID: "pi", Name: "My Pi", Command: "/custom/pi-acp", Args: []string{"--custom"}, Env: map[string]string{"PI_CODING_AGENT_DIR": "/custom/pi"}},
	} {
		t.Run(custom.ID, func(t *testing.T) {
			registry, err := newRegistry([]storage.CustomAgent{custom})
			if err != nil {
				t.Fatal(err)
			}
			descriptor, err := registry.Lookup(custom.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := harness.Launch{Command: custom.Command, Args: custom.Args, Env: custom.Env}
			if descriptor.Name != custom.Name || !reflect.DeepEqual(descriptor.Launch, want) || descriptor.LoginHint == "" {
				t.Fatalf("custom %s settings lost: %+v", custom.ID, descriptor)
			}
			if len(registry.List()) != 4 {
				t.Fatalf("%s registered twice: %+v", custom.ID, registry.List())
			}
		})
	}
}

func TestRegistryStillRejectsDuplicateCustomAgents(t *testing.T) {
	for _, id := range []string{"claude", "pi", "codex", "opencode", "demo"} {
		t.Run(id, func(t *testing.T) {
			custom := storage.CustomAgent{ID: id, Name: id, Command: "bridge"}
			if _, err := newRegistry([]storage.CustomAgent{custom, custom}); err == nil {
				t.Fatal("duplicate registrations were accepted")
			}
		})
	}
}
