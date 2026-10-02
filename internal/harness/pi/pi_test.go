package pi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"agents/internal/harness"
)

func TestDescriptorUsesACPBridge(t *testing.T) {
	descriptor := Descriptor()
	if descriptor.ID != "pi" || descriptor.Name != "Pi" || descriptor.Factory == nil {
		t.Fatalf("incomplete Pi registration: %+v", descriptor)
	}
	if descriptor.Launch.Command != "pi-acp" || len(descriptor.Launch.Args) != 0 || len(descriptor.Launch.Env) != 0 {
		t.Fatalf("expected the bridge, not native Pi or automatic installation: %+v", descriptor.Launch)
	}
	if !strings.Contains(descriptor.LoginHint, "pi-acp --terminal-login") {
		t.Fatalf("missing Pi authentication instructions: %s", descriptor.LoginHint)
	}
}

func TestMissingBridgeExplainsInstallation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	descriptor := Descriptor()
	adapter, err := descriptor.Factory(context.Background(), harness.Options{Workspace: t.TempDir(), Launch: descriptor.Launch}, nil)
	if adapter != nil || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing bridge: adapter=%v, error=%v", adapter, err)
	}
	if !strings.Contains(err.Error(), "npm install -g pi-acp") || !strings.Contains(err.Error(), "Node.js 22+") {
		t.Fatalf("missing setup instructions: %v", err)
	}
}

func TestMissingCommandOverrideIsNotReplaced(t *testing.T) {
	command := filepath.Join(t.TempDir(), "custom-pi-bridge")
	_, err := New(context.Background(), harness.Options{Workspace: t.TempDir(), Launch: harness.Launch{Command: command}}, nil)
	if err == nil || !strings.Contains(err.Error(), strconv.Quote(command)) || strings.Contains(err.Error(), "npm install") {
		t.Fatalf("custom launch settings were not respected: %v", err)
	}
}

func TestWindowsBridgeCommandResolvesAndLaunchesCMD(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows npm installs expose pi-acp.cmd")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pi-acp.cmd"), []byte("@echo off\r\necho bridge-ready\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	path, err := exec.LookPath(Descriptor().Launch.Command)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(path).Output()
	if err != nil || strings.TrimSpace(string(output)) != "bridge-ready" {
		t.Fatalf("launch Pi bridge command: output=%q, error=%v", output, err)
	}
}
