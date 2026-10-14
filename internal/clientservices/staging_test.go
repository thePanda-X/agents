package clientservices

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStagedFilesystemRootsCommitAbortAndCloseHandles(t *testing.T) {
	f := NewFileSystem()
	defer f.Close()
	old, replacement := t.TempDir(), t.TempDir()
	oldPath, newPath := filepath.Join(old, "old.txt"), filepath.Join(replacement, "new.txt")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SetSession("s", []string{old}); err != nil {
		t.Fatal(err)
	}
	previous := f.sessions["s"][0].root
	finish, err := f.StageSession("s", []string{replacement})
	if err != nil {
		t.Fatal(err)
	}
	defer finish(false)
	provisional := f.sessions["s"][0].root
	if _, err := f.Read("s", newPath, 0, 0); err != nil {
		t.Fatalf("setup callbacks cannot access staged roots: %v", err)
	}
	finish(false)
	finish(false)
	if _, err := f.Read("s", oldPath, 0, 0); err != nil {
		t.Fatalf("abort lost the previous roots: %v", err)
	}
	if _, err := provisional.Stat("."); err == nil {
		t.Fatal("aborted provisional handle stayed open")
	}
	finish, err = f.StageSession("s", []string{replacement})
	if err != nil {
		t.Fatal(err)
	}
	defer finish(false)
	finish(true)
	if _, err := previous.Stat("."); err == nil {
		t.Fatal("commit retained replaced directory handles")
	}
	if _, err := f.Read("s", newPath, 0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestStagedNewSessionAbortAndInvalidRoots(t *testing.T) {
	f := NewFileSystem()
	defer f.Close()
	dir := t.TempDir()
	finish, err := f.StageSession("new", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	defer finish(false)
	finish(false)
	if _, exists := f.sessions["new"]; exists {
		t.Fatal("new session authorization survived abort")
	}
	if _, err := f.StageSession("invalid", []string{dir, filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("invalid additional root was accepted")
	}
	// A failed stage must release the setup lock for subsequent operations.
	if err := f.SetSession("valid", []string{dir}); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalCheckpointPreservesExistingAndOtherSessionTerminals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix command fixture")
	}
	f := NewFileSystem()
	defer f.Close()
	for _, id := range []string{"s", "other"} {
		if err := f.SetSession(id, []string{t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}
	m := NewTerminals(f, nil)
	defer m.Close()
	existing, err := m.Create("s", "sh", []string{"-c", "printf existing"}, nil, "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := m.SessionCheckpoint("s")
	created, err := m.Create("s", "sh", []string{"-c", "sleep 60"}, nil, "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.Create("other", "sh", []string{"-c", "printf other"}, nil, "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := m.Output("s", created); err == nil {
		t.Fatal("provisional terminal survived cleanup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for session, id := range map[string]string{"s": existing, "other": other} {
		if _, err := m.Wait(ctx, session, id); err != nil {
			t.Fatalf("cleanup removed a preexisting or unrelated terminal: %v", err)
		}
	}
}
