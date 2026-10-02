// Package clientservices implements the local services an ACP agent may call.
// These boundaries protect client operations; they are not an agent sandbox.
package clientservices

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const MaxTextBytes = 8 << 20

type directory struct {
	path      string
	canonical string
	root      *os.Root
}

type FileSystem struct {
	mu       sync.RWMutex
	sessions map[string][]directory
}

func NewFileSystem() *FileSystem {
	return &FileSystem{sessions: make(map[string][]directory)}
}

// SetSession opens directory handles before publishing them. os.Root enforces
// traversal boundaries during the actual operation, not just a prior path check.
func (f *FileSystem) SetSession(id string, paths []string) error {
	var roots []directory
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			closeDirectories(roots)
			return fmt.Errorf("workspace root must be absolute: %s", path)
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			closeDirectories(roots)
			return err
		}
		root, err := os.OpenRoot(canonical)
		if err != nil {
			closeDirectories(roots)
			return err
		}
		roots = append(roots, directory{path: filepath.Clean(path), canonical: canonical, root: root})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	closeDirectories(f.sessions[id])
	f.sessions[id] = roots
	return nil
}

func (f *FileSystem) locate(id, path string) (directory, string, error) {
	if !filepath.IsAbs(path) {
		return directory{}, "", errors.New("file path must be absolute")
	}
	for _, root := range f.sessions[id] {
		for _, base := range []string{root.path, root.canonical} {
			rel, err := filepath.Rel(base, path)
			if err == nil && filepath.IsLocal(rel) {
				return root, rel, nil
			}
		}
	}
	return directory{}, "", fmt.Errorf("path is outside this session's workspace roots: %s", path)
}

func (f *FileSystem) Read(id, path string, line, limit uint32) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	root, rel, err := f.locate(id, path)
	if err != nil {
		return "", err
	}
	file, err := root.root.OpenFile(rel, os.O_RDONLY|nonblockingReadFlag(), 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("only regular text files can be read")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxTextBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxTextBytes {
		return "", errors.New("text file exceeds 8 MiB limit")
	}
	if bytes.ContainsRune(data, 0) || !utf8.Valid(data) {
		return "", errors.New("file is not UTF-8 text")
	}
	if line == 0 && limit == 0 {
		return string(data), nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	start := 0
	if line > 0 {
		start = int(line) - 1
	}
	if start >= len(lines) {
		return "", nil
	}
	end := len(lines)
	if limit > 0 {
		end = min(end, start+int(limit))
	}
	return strings.Join(lines[start:end], ""), nil
}

func (f *FileSystem) Write(id, path, content string) error {
	if len(content) > MaxTextBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return errors.New("write requires UTF-8 text under 8 MiB")
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	root, rel, err := f.locate(id, path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, statErr := root.root.Lstat(rel); statErr == nil {
		// Replacing a symlink atomically would edit the link rather than its
		// target. Refuse instead of silently changing filesystem semantics.
		if !info.Mode().IsRegular() {
			return errors.New("write target must be a regular file, not a symlink or directory")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	temp := filepath.Join(filepath.Dir(rel), ".agents-"+rand.Text())
	file, err := root.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer root.root.Remove(temp)
	if _, err = io.WriteString(file, content); err != nil {
		file.Close()
		return err
	}
	if err = file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return root.root.Rename(temp, rel)
}

// Directory validates a command's working directory. It cannot constrain what
// the command itself accesses; OS sandboxing belongs to the selected harness.
func (f *FileSystem) Directory(id, path string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if path == "" && len(f.sessions[id]) > 0 {
		path = f.sessions[id][0].path
	}
	root, rel, err := f.locate(id, path)
	if err != nil {
		return "", err
	}
	info, err := root.root.Stat(rel)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("command cwd is not a directory")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if _, _, err = f.locate(id, canonical); err != nil {
		return "", err
	}
	return canonical, nil
}

func (f *FileSystem) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, roots := range f.sessions {
		closeDirectories(roots)
		delete(f.sessions, id)
	}
}

func closeDirectories(roots []directory) {
	for _, root := range roots {
		_ = root.root.Close()
	}
}

func (f *FileSystem) RemoveSession(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	closeDirectories(f.sessions[id])
	delete(f.sessions, id)
}
