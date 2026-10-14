// Package storage persists client preferences and drafts, never credentials or
// an authoritative copy of an agent's conversation history.
package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"agents/internal/harness"
)

type CustomAgent struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

type Settings struct {
	Harness       string            `json:"harness"`
	Mouse         bool              `json:"mouse"`
	ShowThoughts  bool              `json:"showThoughts"`
	VimNavigation bool              `json:"vimNavigation,omitempty"`
	Compact       bool              `json:"compact,omitempty"`
	Appearance    string            `json:"appearance,omitempty"`
	ReducedMotion bool              `json:"reducedMotion,omitempty"`
	Keybindings   map[string]string `json:"keybindings,omitempty"`
	Agents        []CustomAgent     `json:"agents,omitempty"`
	MCP           json.RawMessage   `json:"mcpServers,omitempty"`
}

type WorkspaceState struct {
	Session harness.SessionInfo `json:"session"`
	Drafts  map[string]string   `json:"drafts,omitempty"`
}

type Snapshot struct {
	Version    int                       `json:"version"`
	Revision   uint64                    `json:"revision"`
	Workspaces map[string]WorkspaceState `json:"workspaces"`
}

type Store struct {
	Directory    string
	mu           sync.Mutex
	lastRevision uint64
	initialized  bool
	settings     Settings
	snapshot     Snapshot
}

func DefaultDirectory() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "agents"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agents"), nil
}

func (s *Store) Load() (Settings, Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, snapshot, err := s.load()
	if err == nil {
		s.settings, s.snapshot = cloneSettings(settings), cloneSnapshot(snapshot)
		s.initialized = true
	}
	return settings, snapshot, err
}

func (s *Store) load() (Settings, Snapshot, error) {
	settings := Settings{Harness: "opencode", Mouse: true, Keybindings: make(map[string]string)}
	snapshot := Snapshot{Version: 1, Workspaces: make(map[string]WorkspaceState)}
	if err := s.read("settings.json", &settings); err != nil {
		return settings, snapshot, err
	}
	if err := s.read("state.json", &snapshot); err != nil {
		return settings, snapshot, err
	}
	if snapshot.Version != 1 {
		return settings, snapshot, fmt.Errorf("unsupported client state version %d", snapshot.Version)
	}
	if snapshot.Workspaces == nil {
		snapshot.Workspaces = make(map[string]WorkspaceState)
	}
	return settings, snapshot, nil
}

func (s *Store) read(name string, target any) error {
	data, err := os.ReadFile(filepath.Join(s.Directory, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func (s *Store) Save(settings Settings, snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Autosaves may complete out of order. An older snapshot must never
	// overwrite a newer shutdown save.
	if snapshot.Revision < s.lastRevision {
		return nil
	}
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return err
	}
	lock, err := lockStore(filepath.Join(s.Directory, ".state.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	currentSettings, currentSnapshot, err := s.load()
	if err != nil {
		return err
	}
	mergedSettings := mergeSettings(currentSettings, s.settings, settings)
	if !s.initialized {
		mergedSettings = settings
	}
	mergedSnapshot := mergeSnapshot(currentSnapshot, s.snapshot, snapshot)
	if err := s.write("settings.json", mergedSettings); err != nil {
		return err
	}
	if err := s.write("state.json", mergedSnapshot); err != nil {
		return err
	}
	s.lastRevision = snapshot.Revision
	// Keep this client's submitted baseline, not the merged disk state. Otherwise
	// its next save would mistake another client's unseen additions for deletions.
	s.settings, s.snapshot = cloneSettings(settings), cloneSnapshot(snapshot)
	s.initialized = true
	return nil
}

func (s *Store) write(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.Directory, ".agents-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(s.Directory, name))
}

func Key(workspace, harnessID string) string { return harnessID + ":" + workspace }
