package transcript

import (
	"encoding/json"
	"strings"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/tui/theme"
)

// These are presentation-only fallbacks, not authoritative file changes. Never
// read the working tree to guess old content: it may already have been changed.
func toolDiffPreviews(t *app.ToolState) []string {
	if len(t.Diffs) != 0 {
		return nil
	} // Prefer the harness's structured diffs.
	name := strings.ToLower(t.Title)
	if fields := strings.Fields(name); len(fields) > 0 {
		name = fields[0]
	}
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	write := name == "write" || name == "write_file" || name == "writefile"
	edit := write || t.Kind == "edit" || name == "edit" || name == "edit_file" || name == "editfile" || name == "multiedit" || name == "patch" || name == "apply_patch"
	if !edit {
		return nil
	}
	decode := func(raw json.RawMessage) map[string]json.RawMessage {
		var object map[string]json.RawMessage
		if len(raw) <= 128<<10 {
			_ = json.Unmarshal(raw, &object)
		}
		return object
	}
	input, output := decode(t.Input), decode(t.Output)
	get := func(object map[string]json.RawMessage, keys ...string) (string, bool) {
		for _, key := range keys {
			var text string
			if raw, ok := object[key]; ok && json.Unmarshal(raw, &text) == nil {
				return text, true
			}
		}
		return "", false
	}
	// Prefer a reported unified diff over reconstructing input snippets.
	metadata := decode(output["metadata"])
	for _, object := range []map[string]json.RawMessage{output, metadata} {
		if diff, ok := get(object, "diff", "patch", "patchText", "patch_text"); ok && isPatch(diff) {
			return []string{theme.Quiet.Render("Reported patch"), renderPatch(diff)}
		}
	}
	fileDiff := decode(metadata["filediff"])
	file, _ := get(fileDiff, "file", "filePath", "path")
	before, hasBefore := get(fileDiff, "before")
	after, hasAfter := get(fileDiff, "after")
	if file != "" && hasBefore && hasAfter {
		return []string{theme.Quiet.Render("Reported file diff"), RenderDiff(harness.Diff{Path: file, OldText: before, NewText: after})}
	}
	for _, payload := range []struct {
		raw   json.RawMessage
		label string
	}{{t.Output, "Reported patch"}, {t.Input, "Requested patch"}} {
		var patch string
		if len(payload.raw) <= 128<<10 && json.Unmarshal(payload.raw, &patch) == nil && isPatch(patch) {
			return []string{theme.Quiet.Render(payload.label), renderPatch(patch)}
		}
	}
	if diff, ok := get(input, "diff", "patch", "patchText", "patch_text"); ok && isPatch(diff) {
		return []string{theme.Quiet.Render("Requested patch"), renderPatch(diff)}
	}
	if len(t.Text) <= 128<<10 && isPatch(t.Text) {
		return []string{theme.Quiet.Render("Reported patch"), renderPatch(t.Text)}
	}
	// Some apply_patch tools put their entire patch in the content field.
	if !write {
		if patch, ok := get(input, "content"); ok && isPatch(patch) {
			return []string{theme.Quiet.Render("Requested patch"), renderPatch(patch)}
		}
	}
	path, _ := get(input, "file_path", "filePath", "path")
	if path == "" {
		return nil
	}
	old, hasOld := get(input, "old_string", "oldString", "oldText", "old_text")
	newText, hasNew := get(input, "new_string", "newString", "newText", "new_text")
	if hasOld && hasNew {
		return []string{theme.Quiet.Render("Requested edit · supplied text only"), RenderDiff(harness.Diff{Path: path, OldText: old, NewText: newText})}
	}
	if write || t.Kind == "edit" {
		if content, ok := get(input, "content"); ok {
			return []string{theme.Quiet.Render("Requested write · previous contents unavailable · " + theme.Clean(path)), renderPatch("+" + strings.ReplaceAll(content, "\n", "\n+"))}
		}
	}
	return nil
}

func isPatch(text string) bool {
	return strings.HasPrefix(text, "*** Begin Patch\n") || strings.HasPrefix(text, "diff --git ") || strings.HasPrefix(text, "--- ") && strings.Contains(text, "\n+++ ") && strings.Contains(text, "\n@@")
}

func renderPatch(text string) string {
	lines := strings.Split(theme.Clean(text), "\n")
	for index, line := range lines {
		switch {
		case strings.HasPrefix(line, "+"):
			lines[index] = theme.Success.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[index] = theme.Error.Render(line)
		case strings.HasPrefix(line, "@@"), strings.HasPrefix(line, "*** "), strings.HasPrefix(line, "diff --git "):
			lines[index] = theme.Accent.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}
