package tui

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"agents/internal/tui/theme"

	tea "charm.land/bubbletea/v2"
)

type filePreviewMsg struct {
	path, text string
	generation uint64
}

// Previews are bounded, asynchronous, and scoped to the workspace. Never read
// files during View or let a stale preview populate a newly opened picker.
func (m *Model) previewFile() tea.Cmd {
	if m.list == nil {
		return nil
	}
	item, ok := m.list.Selected()
	if !ok || item.ID == m.filePreviewPath {
		return nil
	}
	m.filePreviewPath = item.ID
	if item.Preview != "" {
		return nil
	}
	workspace := m.opts.Agent.Workspace
	if c := m.state.Active(); c != nil {
		workspace = c.Info.Workspace
	}
	path, generation := item.ID, m.filePickerGeneration
	return func() tea.Msg {
		result := filePreviewMsg{path: path, generation: generation}
		root, err := os.OpenRoot(workspace)
		if err != nil {
			result.text = "Cannot preview: " + err.Error()
			return result
		}
		defer root.Close()
		info, err := root.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			result.text = "No preview for missing or non-regular files."
			return result
		}
		file, err := root.Open(path)
		if err != nil {
			result.text = "Cannot preview: " + err.Error()
			return result
		}
		defer file.Close()
		info, err = file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			result.text = "No preview for non-regular files."
			return result
		}
		data, err := io.ReadAll(io.LimitReader(file, 8193))
		if err != nil {
			result.text = "Cannot preview: " + err.Error()
			return result
		}
		truncated := len(data) > 8192
		if truncated {
			data = data[:8192]
			for len(data) > 0 && !utf8.Valid(data) && len(data) > 8188 {
				data = data[:len(data)-1]
			}
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			result.text = "Binary file · no text preview"
			return result
		}
		result.text = theme.Clean(string(data))
		if result.text == "" {
			result.text = "Empty file"
		}
		if truncated {
			result.text += "\n… preview limited to 8 KiB"
		}
		return result
	}
}
