package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestConcurrentClientsMergeWorkspacesSettingsAndDrafts(t *testing.T) {
	dir := t.TempDir()
	a, b := &Store{Directory: dir}, &Store{Directory: dir}
	settingsA, snapshotA, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	settingsB, snapshotB, err := b.Load()
	if err != nil {
		t.Fatal(err)
	}
	snapshotA.Workspaces["a"] = WorkspaceState{Drafts: map[string]string{"s1": "draft A"}}
	settingsA.Compact = true
	settingsA.Keybindings["copy"] = "alt+y"
	snapshotB.Workspaces["b"] = WorkspaceState{Drafts: map[string]string{"s2": "draft B"}}
	settingsB.ReducedMotion = true
	settingsB.Keybindings["palette"] = "alt+p"
	for i := range 3 {
		snapshotA.Revision++
		snapshotB.Revision++
		if err := a.Save(settingsA, snapshotA); err != nil {
			t.Fatal(err)
		}
		if err := b.Save(settingsB, snapshotB); err != nil {
			t.Fatal(err)
		}
		// Repeated saves with stale local views must not remove foreign data.
		snapshotA.Workspaces["a"].Drafts["s1"] = fmt.Sprintf("draft A %d", i+1)
	}
	settings, snapshot, err := (&Store{Directory: dir}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Workspaces["a"].Drafts["s1"] != "draft A 2" || snapshot.Workspaces["b"].Drafts["s2"] != "draft B" {
		t.Fatalf("lost concurrent drafts: %+v", snapshot.Workspaces)
	}
	if !settings.Compact || !settings.ReducedMotion || settings.Keybindings["copy"] != "alt+y" || settings.Keybindings["palette"] != "alt+p" {
		t.Fatalf("lost concurrent settings: %+v", settings)
	}
}

func TestConcurrentClientsMergeDraftDeletionInSameWorkspace(t *testing.T) {
	dir := t.TempDir()
	seed := &Store{Directory: dir}
	if err := seed.Save(Settings{Harness: "opencode"}, Snapshot{Version: 1, Workspaces: map[string]WorkspaceState{
		"shared": {Drafts: map[string]string{"delete": "old", "keep": "old"}},
	}}); err != nil {
		t.Fatal(err)
	}
	a, b := &Store{Directory: dir}, &Store{Directory: dir}
	sa, wa, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	sb, wb, err := b.Load()
	if err != nil {
		t.Fatal(err)
	}
	delete(wa.Workspaces["shared"].Drafts, "delete")
	wb.Workspaces["shared"].Drafts["keep"] = "updated"
	wb.Workspaces["shared"].Drafts["new"] = "new draft"
	if err := b.Save(sb, wb); err != nil {
		t.Fatal(err)
	}
	if err := a.Save(sa, wa); err != nil {
		t.Fatal(err)
	}
	_, got, err := (&Store{Directory: dir}).Load()
	if err != nil {
		t.Fatal(err)
	}
	drafts := got.Workspaces["shared"].Drafts
	if _, exists := drafts["delete"]; exists || drafts["keep"] != "updated" || drafts["new"] != "new draft" {
		t.Fatalf("draft merge lost edits or resurrected deletion: %+v", drafts)
	}
}

func TestSaveDoesNotReplaceMalformedConcurrentState(t *testing.T) {
	s := &Store{Directory: t.TempDir()}
	settings, snapshot, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Directory, "state.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(settings, snapshot); err == nil {
		t.Fatal("save silently replaced malformed concurrent state")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{broken" {
		t.Fatalf("malformed state was overwritten: %q, %v", data, err)
	}
}

func TestConcurrentStoreProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	for _, id := range []string{"a", "b"} {
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStoreProcessHelper$")
		cmd.Env = append(os.Environ(), "AGENTS_STORE_TEST_DIR="+dir, "AGENTS_STORE_TEST_ID="+id)
		output := new(bytes.Buffer)
		cmd.Stdout, cmd.Stderr = output, output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		commands, outputs = append(commands, cmd), append(outputs, output)
	}
	for _, id := range []string{"a", "b"} {
		if err := waitForStoreTestFile(ctx, filepath.Join(dir, id+".ready")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "start"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("store process: %v\n%s", err, outputs[i])
		}
	}
	_, snapshot, err := (&Store{Directory: dir}).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if got := snapshot.Workspaces[id].Drafts["s"]; got != "draft 19" {
			t.Fatalf("process %s lost its final draft: %q", id, got)
		}
	}
}

func TestStoreProcessHelper(t *testing.T) {
	dir, id := os.Getenv("AGENTS_STORE_TEST_DIR"), os.Getenv("AGENTS_STORE_TEST_ID")
	if dir == "" || id == "" {
		return
	}
	s := &Store{Directory: dir}
	settings, snapshot, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".ready"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := waitForStoreTestFile(ctx, filepath.Join(dir, "start")); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		snapshot.Revision++
		snapshot.Workspaces[id] = WorkspaceState{Drafts: map[string]string{"s": fmt.Sprintf("draft %d", i)}}
		if err := s.Save(settings, snapshot); err != nil {
			t.Fatal(err)
		}
	}
}

func waitForStoreTestFile(ctx context.Context, path string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
