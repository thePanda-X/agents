package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStoreRoundTripAndPrivatePermissions(t *testing.T) {
	s := &Store{Directory: t.TempDir()}
	settings, snapshot, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Harness != "opencode" || !settings.Mouse {
		t.Fatal("unexpected defaults")
	}
	snapshot.Workspaces["test"] = WorkspaceState{Drafts: map[string]string{"s": "unsent"}}
	if err := s.Save(settings, snapshot); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := s.Load()
	if err != nil || loaded.Workspaces["test"].Drafts["s"] != "unsent" {
		t.Fatalf("roundtrip: %+v, %v", loaded, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(s.Directory, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("state is not private: %o", info.Mode().Perm())
		}
	}
}

func TestOlderAutosaveCannotReplaceNewerSnapshot(t *testing.T) {
	s := &Store{Directory: t.TempDir()}
	settings := Settings{Harness: "opencode"}
	if err := s.Save(settings, Snapshot{Version: 1, Revision: 2, Workspaces: map[string]WorkspaceState{"new": {}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(settings, Snapshot{Version: 1, Revision: 1, Workspaces: map[string]WorkspaceState{"old": {}}}); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := loaded.Workspaces["new"]; !exists {
		t.Fatal("older snapshot replaced newer state")
	}
}

func TestMalformedSettingsAreNotSilentlyOverwritten(t *testing.T) {
	s := &Store{Directory: t.TempDir()}
	if err := os.WriteFile(filepath.Join(s.Directory, "settings.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load(); err == nil {
		t.Fatal("malformed settings accepted")
	}
}
