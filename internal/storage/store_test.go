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

func TestSessionNamesRoundTrip(t *testing.T) {
	s := &Store{Directory: t.TempDir()}
	settings, snapshot, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SessionNames["opencode:s1"] = "My name"
	if err := s.Save(settings, snapshot); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := s.Load()
	if err != nil || loaded.SessionNames["opencode:s1"] != "My name" {
		t.Fatalf("roundtrip: %+v, %v", loaded, err)
	}
}

func TestMissingSessionNamesLoadAsEmpty(t *testing.T) {
	s := &Store{Directory: t.TempDir()}
	if err := os.WriteFile(filepath.Join(s.Directory, "state.json"), []byte(`{"version":1,"revision":1,"workspaces":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, snapshot, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SessionNames == nil {
		t.Fatal("session names map not initialized")
	}
}

func TestSessionNamesMergePreservesOtherClients(t *testing.T) {
	current := Snapshot{Workspaces: map[string]WorkspaceState{}, SessionNames: map[string]string{"a:s": "A", "b:s": "B"}}
	before := Snapshot{SessionNames: map[string]string{"a:s": "A"}}
	after := Snapshot{SessionNames: map[string]string{"a:s": "A2"}}
	merged := mergeSnapshot(current, before, after)
	if merged.SessionNames["a:s"] != "A2" || merged.SessionNames["b:s"] != "B" {
		t.Fatalf("merge lost entries: %+v", merged.SessionNames)
	}
	// Removing an entry this client previously wrote deletes exactly that entry.
	after2 := Snapshot{SessionNames: map[string]string{}}
	merged2 := mergeSnapshot(current, before, after2)
	if _, ok := merged2.SessionNames["a:s"]; ok {
		t.Fatal("deleted name survived merge")
	}
	if merged2.SessionNames["b:s"] != "B" {
		t.Fatal("unrelated client name lost")
	}
}
