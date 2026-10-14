package opencode

import (
	"encoding/json"
	"reflect"
	"testing"

	"agents/internal/harness"
)

func TestDefaultLaunchAllowsAllBuildToolsWithoutMutatingOverrides(t *testing.T) {
	t.Setenv(configEnv, "")
	original := harness.Launch{Command: "/custom/opencode", Args: []string{"--print-logs", "acp"}, Env: map[string]string{"OTHER": "preserved"}}
	launch := defaultLaunch(original)
	if launch.Command != original.Command || !reflect.DeepEqual(launch.Args, original.Args) || launch.Env["OTHER"] != "preserved" {
		t.Fatalf("launch overrides lost: %+v", launch)
	}
	if _, mutated := original.Env[configEnv]; mutated {
		t.Fatal("shared launch environment was mutated")
	}
	type rule struct {
		Action, Resource, Effect string
	}
	var config struct {
		DefaultAgent string `json:"default_agent"`
		Permissions  []rule
		Agents       map[string]struct{ Permissions []rule }
	}
	if err := json.Unmarshal([]byte(launch.Env[configEnv]), &config); err != nil {
		t.Fatal(err)
	}
	want := []rule{{Action: "*", Resource: "*", Effect: "allow"}}
	if config.DefaultAgent != "build" || !reflect.DeepEqual(config.Permissions, want) || !reflect.DeepEqual(config.Agents["build"].Permissions, want) {
		t.Fatalf("missing permissive V2 build policy: %+v", config)
	}
	if len(config.Agents) != 1 {
		t.Fatal("other native agent policies were overridden")
	}
}

func TestDefaultLaunchPreservesExplicitInlineConfig(t *testing.T) {
	for _, tt := range []struct {
		name, inherited string
		env             map[string]string
	}{
		{"inherited JSONC", "{ /* preserve plugins */ \"model\":\"provider/model\" }", nil},
		{"explicit config", "", map[string]string{configEnv: "{\"permissions\":[]}"}},
		{"explicit empty override", "{\"model\":\"provider/model\"}", map[string]string{configEnv: ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(configEnv, tt.inherited)
			original := harness.Launch{Command: "opencode", Args: []string{"acp"}, Env: tt.env}
			if got := defaultLaunch(original); !reflect.DeepEqual(got, original) {
				t.Fatalf("explicit inline configuration changed: %+v", got)
			}
		})
	}
}
