package clientservices

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTerminalLifecycleAndRetainedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix command fixture")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh unavailable")
	}
	f := NewFileSystem()
	defer f.Close()
	if err := f.SetSession("s", []string{t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	m := NewTerminals(f, nil)
	defer m.Close()
	id, err := m.Create("s", "sh", []string{"-c", "printf 'héllo'; printf ' error' >&2"}, nil, "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := m.Wait(ctx, "s", id)
	if err != nil || status.Code != 0 {
		t.Fatalf("wait: %+v, %v", status, err)
	}
	output, err := m.Output("s", id)
	if err != nil || !strings.Contains(output.Output, "héllo") || !strings.Contains(output.Output, "error") {
		t.Fatalf("output: %+v, %v", output, err)
	}
	if _, err := m.Output("other", id); err == nil {
		t.Fatal("cross-session terminal access allowed")
	}
	if err := m.Release("s", id); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Output("s", id); err == nil {
		t.Fatal("released terminal still available")
	}
}

func TestTerminalKillAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix command fixture")
	}
	f := NewFileSystem()
	defer f.Close()
	if err := f.SetSession("s", []string{t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	m := NewTerminals(f, nil)
	defer m.Close()
	id, err := m.Create("s", "sh", []string{"-c", "sleep 60"}, nil, "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Wait(ctx, "s", id); err == nil {
		t.Fatal("cancelled wait succeeded")
	}
	if err := m.Kill("s", id); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if _, err := m.Wait(deadline, "s", id); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalBufferTruncatesAtUTF8Boundary(t *testing.T) {
	tm := &terminal{limit: 5, notify: make(chan struct{}, 1)}
	_, _ = tm.Write([]byte("a界界"))
	snapshot := tm.snapshot()
	if !snapshot.Truncated || len(snapshot.Output) > 5 || !utf8.ValidString(snapshot.Output) {
		t.Fatalf("invalid bounded output: %+v", snapshot)
	}
}

func TestMergeEnvReplacesInsteadOfDuplicating(t *testing.T) {
	env := MergeEnv([]string{"A=old", "B=keep"}, map[string]string{"A": "new"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "A=old") || strings.Count(joined, "A=") != 1 {
		t.Fatalf("ambiguous environment: %v", env)
	}
}
