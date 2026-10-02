package tui

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type branchMsg struct {
	workspace string
	branch    string
}

func (m *Model) activeWorkspace() string {
	if c := m.state.Active(); c != nil {
		return c.Info.Workspace
	}
	return m.opts.Agent.Workspace
}

func (m *Model) refreshBranch(now time.Time) tea.Cmd {
	workspace := m.activeWorkspace()
	if m.branchPending || (workspace == m.branchWorkspace && now.Sub(m.branchChecked) < 5*time.Second) {
		return nil
	}
	m.branchPending, m.branchChecked = true, now
	ctx := m.opts.Context
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "-C", workspace, "symbolic-ref", "--quiet", "--short", "HEAD")
		output, err := cmd.Output()
		branch := ""
		if err == nil {
			branch = strings.TrimSpace(string(output))
		}
		return branchMsg{workspace: workspace, branch: branch}
	}
}
