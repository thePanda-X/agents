package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalDirectoryUsesInvocationDirectory(t *testing.T) {
	repository := t.TempDir()
	workspace := filepath.Join(repository, "packages", "example")
	for _, path := range []string{filepath.Join(repository, ".git"), workspace} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(workspace)

	got, err := canonicalDirectory(".")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspace = %q, want launch directory %q", got, want)
	}
}

func TestCanonicalDirectorySupportsExplicitOverride(t *testing.T) {
	launchDirectory := t.TempDir()
	override := t.TempDir()
	t.Chdir(launchDirectory)

	got, err := canonicalDirectory(override)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(override)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspace = %q, want override %q", got, want)
	}
}
