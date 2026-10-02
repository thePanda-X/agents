package clientservices

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileSystemReadsRangesAndPreservesWriteMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	f := NewFileSystem()
	defer f.Close()
	if err := f.SetSession("s", []string{dir}); err != nil {
		t.Fatal(err)
	}
	text, err := f.Read("s", path, 2, 1)
	if err != nil || text != "two\n" {
		t.Fatalf("range read: %q, %v", text, err)
	}
	if err := f.Write("s", path, "updated\n"); err != nil {
		t.Fatal(err)
	}
	if text, err := f.Read("s", path, 0, 0); err != nil || text != "updated\n" {
		t.Fatalf("updated contents: %q, %v", text, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows exposes only a subset of POSIX mode bits. Verify preservation of
	// the mode actually supported by the filesystem rather than a Unix constant.
	if info.Mode().Perm() != original.Mode().Perm() {
		t.Fatalf("permissions changed: %o -> %o", original.Mode().Perm(), info.Mode().Perm())
	}
}

func TestFileSystemRejectsTraversalSymlinksAndCrossSessionAccess(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewFileSystem()
	defer f.Close()
	if err := f.SetSession("s", []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Read("s", secret, 0, 0); err == nil {
		t.Fatal("outside read allowed")
	}
	if _, err := f.Read("unknown", secret, 0, 0); err == nil {
		t.Fatal("unknown session read allowed")
	}
	if _, err := f.Read("s", "relative", 0, 0); err == nil {
		t.Fatal("relative path allowed")
	}
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := f.Read("s", filepath.Join(link, "secret"), 0, 0); err == nil {
		t.Fatal("symlink escape allowed")
	}
	if err := f.Write("s", filepath.Join(link, "new"), "bad"); err == nil {
		t.Fatal("symlink write escape allowed")
	}
}

func TestFileSystemRejectsBinaryAndOversizedText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary")
	if err := os.WriteFile(path, []byte{1, 0, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewFileSystem()
	defer f.Close()
	if err := f.SetSession("s", []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Read("s", path, 0, 0); err == nil {
		t.Fatal("binary read allowed")
	}
	if err := f.Write("s", path, string(make([]byte, MaxTextBytes+1))); err == nil {
		t.Fatal("oversized write allowed")
	}
}
