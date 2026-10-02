package claude

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"agents/internal/harness"
)

func TestDescriptorUsesACPBridge(t *testing.T) {
	descriptor := Descriptor()
	if descriptor.ID != "claude" || descriptor.Name != "Claude Code" || descriptor.Factory == nil {
		t.Fatalf("incomplete Claude registration: %+v", descriptor)
	}
	if descriptor.Launch.Command != "claude-agent-acp" || len(descriptor.Launch.Args) != 0 || len(descriptor.Launch.Env) != 0 {
		t.Fatalf("expected the bridge, not native Claude or automatic installation: %+v", descriptor.Launch)
	}
	if !strings.Contains(descriptor.LoginHint, "--cli auth login") || !strings.Contains(descriptor.LoginHint, "ANTHROPIC_API_KEY") {
		t.Fatalf("missing bridge authentication instructions: %s", descriptor.LoginHint)
	}
}

func TestMissingBridgeExplainsInstallation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	descriptor := Descriptor()
	adapter, err := descriptor.Factory(context.Background(), harness.Options{Workspace: t.TempDir(), Launch: descriptor.Launch}, nil)
	if adapter != nil || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing bridge: adapter=%v, error=%v", adapter, err)
	}
	if !strings.Contains(err.Error(), "npm install -g @agentclientprotocol/claude-agent-acp") || !strings.Contains(err.Error(), "Node.js 22+") {
		t.Fatalf("missing setup instructions: %v", err)
	}
}

func TestMissingCommandOverrideIsNotReplaced(t *testing.T) {
	command := filepath.Join(t.TempDir(), "custom-claude-bridge")
	_, err := New(context.Background(), harness.Options{Workspace: t.TempDir(), Launch: harness.Launch{Command: command}}, nil)
	if err == nil || !strings.Contains(err.Error(), strconv.Quote(command)) || strings.Contains(err.Error(), "npm install") {
		t.Fatalf("custom launch settings were not respected: %v", err)
	}
}
