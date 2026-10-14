package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClipboardCopyCannotBypassWriterLimit(t *testing.T) {
	buffer := &clipboardBuffer{limit: 4}
	// Hide the source's WriterTo, as an exec stdout pipe does, to exercise
	// io.Copy's destination optimization rather than a direct Write call.
	reader := struct{ io.Reader }{strings.NewReader("12345")}
	if _, err := io.Copy(buffer, reader); err == nil || buffer.Len() > 4 {
		t.Fatal("io.Copy bypassed the clipboard output limit")
	}
}

func TestClipboardTextIsBoundedAndCancellableWithoutReadingDesktop(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix fake clipboard utility")
	}
	dir := t.TempDir()
	// Never touch the real clipboard: all supported Unix helpers are shadowed
	// with a script that reads only this test's temporary fixture.
	for _, name := range []string{"pbpaste", "wl-paste", "xclip", "xsel"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\ncat \"$AGENTS_CLIPBOARD_TEXT_FILE\"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "text.txt")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DISPLAY", "test-display")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("AGENTS_CLIPBOARD_TEXT_FILE", path)
	text := strings.Repeat("\ttext with 界\n", 10000)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadClipboardText(context.Background())
	if err != nil || got != text {
		t.Fatalf("clipboard text was changed or truncated: bytes=%d, err=%v", len(got), err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxClipboardImageBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadClipboardText(context.Background()); err == nil || got != "" {
		t.Fatal("oversized clipboard content was silently truncated or accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadClipboardText(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("clipboard cancellation semantics lost: %v", err)
	}
}
