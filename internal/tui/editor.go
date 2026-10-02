package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"agents/internal/harness"
	"agents/internal/tui/overlays"
	"agents/internal/tui/theme"

	tea "charm.land/bubbletea/v2"
)

type reportedChange struct {
	Entry int
	Diff  harness.Diff
}
type handoffMsg struct{ err error }

func editorCommand() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return "vi"
}

func (m *Model) openReportedFile() tea.Cmd {
	paths := append([]string(nil), m.infoPaths...)
	if len(paths) == 0 {
		if entry := m.selectedEntry(); entry != nil && entry.Tool != nil {
			paths = append(paths, entry.Tool.Locations...)
			for _, diff := range entry.Tool.Diffs {
				paths = append(paths, diff.Path)
			}
		}
	}
	var items []overlays.Item
	seen := make(map[string]bool)
	for _, path := range paths {
		if !seen[path] {
			items = append(items, overlays.Item{ID: path, Label: theme.Clean(path), Value: path})
			seen[path] = true
		}
	}
	if len(items) == 0 {
		m.state.Notice = "No reported file location for this entry"
		return nil
	}
	// Review even a single target before opening agent-provided paths.
	m.openList("editor_files", "Open current workspace file in editor", items)
	m.list.Description = "Enter opens the current file, not the historical reported version. Only regular files inside the workspace are allowed."
	return nil
}

func resolveReportedPath(workspace, location string) (string, int, error) {
	line := 0
	if colon := strings.LastIndex(location, ":"); colon >= 0 {
		if number, err := strconv.Atoi(location[colon+1:]); err == nil && number > 0 {
			location, line = location[:colon], number
		}
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", 0, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", 0, err
	}
	if !filepath.IsAbs(location) {
		location = filepath.Join(root, location)
	}
	path, err := filepath.EvalSymlinks(location)
	if err != nil {
		return "", 0, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", 0, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", 0, fmt.Errorf("reported file is outside the active workspace")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("reported location is not a regular file")
	}
	return path, line, nil
}

func (m *Model) editReportedPath(location string) tea.Cmd {
	workspace := m.opts.Agent.Workspace
	if c := m.state.Active(); c != nil {
		workspace = c.Info.Workspace
	}
	path, line, err := resolveReportedPath(workspace, location)
	if err != nil {
		m.state.LastError = err
		return nil
	}
	var cmd *exec.Cmd
	if line > 0 {
		cmd = exec.Command("sh", "-c", "exec "+editorCommand()+" \"$1\" \"$2\"", "agents-file", fmt.Sprintf("+%d", line), path)
	} else {
		cmd = exec.Command("sh", "-c", "exec "+editorCommand()+" \"$1\"", "agents-file", path)
	}
	cmd.Dir = workspace
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return handoffMsg{err} })
}

func (m *Model) exportSelected() tea.Cmd {
	entry := m.selectedEntry()
	if entry == nil {
		return nil
	}
	file, err := os.CreateTemp("", "agents-output-*.md")
	if err != nil {
		m.state.LastError = err
		return nil
	}
	path := file.Name()
	_, writeErr := file.WriteString(theme.Clean(m.entryText(entry)))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		if writeErr != nil {
			m.state.LastError = writeErr
		} else {
			m.state.LastError = closeErr
		}
		return nil
	}
	cmd := exec.Command("sh", "-c", "exec "+editorCommand()+" \"$1\"", "agents-output", path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { os.Remove(path); return handoffMsg{err} })
}
