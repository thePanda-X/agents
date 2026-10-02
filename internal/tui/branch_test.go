package tui

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceBranchHeader(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	git("init")
	git("symbolic-ref", "HEAD", "refs/heads/feature/header")
	m := testModel(t)
	m.state.Active().Info.Workspace = root
	now := time.Now()
	msg := m.refreshBranch(now)().(branchMsg)
	m.Update(msg)
	if msg.branch != "feature/header" || !strings.Contains(m.header(500), ":feature/header") {
		t.Fatalf("missing workspace branch: %q", m.header(500))
	}
	if cmd := m.refreshBranch(now.Add(time.Second)); cmd != nil {
		t.Fatal("branch lookup was not throttled")
	}
	git("symbolic-ref", "HEAD", "refs/heads/main")
	m.Update(m.refreshBranch(now.Add(6 * time.Second))())
	if !strings.Contains(m.header(500), ":main") {
		t.Fatalf("branch did not refresh: %q", m.header(500))
	}
	m.state.Active().Info.Workspace = t.TempDir()
	if strings.Contains(m.header(500), ":main") {
		t.Fatal("old workspace branch leaked into header")
	}
	m.Update(branchMsg{workspace: root, branch: "stale"})
	if strings.Contains(m.header(500), ":stale") {
		t.Fatal("stale branch result was applied")
	}
	m.Update(m.refreshBranch(now.Add(7 * time.Second))())
	if m.branch != "" {
		t.Fatalf("non-repository has branch %q", m.branch)
	}
}
