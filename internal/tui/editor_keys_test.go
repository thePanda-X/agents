package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEditorShortcutUsesCtrlGAndLeavesCtrlEForLineEnd(t *testing.T) {
	m := testModel(t)
	if m.actionFor("ctrl+g") != "editor" || m.actionFor("ctrl+e") != "" || m.binding("editor") != "Ctrl+G" {
		t.Fatal("editor default binding was not moved to Ctrl+G")
	}
	m.input.Focus()
	m.input.SetValue("first line\nsecond line")
	m.input.SetCursorColumn(2)
	m.handleKey(ctrl('e'))
	if m.input.Line() != 1 || m.input.Column() != len("second line") || m.input.Value() != "first line\nsecond line" {
		t.Fatal("Ctrl+E did not preserve the draft and move to the current line's end")
	}
	if len(m.recentActions) != 0 {
		t.Fatal("Ctrl+E dispatched a client action instead of composer navigation")
	}
	m.input.SetCursorColumn(0)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	if m.input.Column() == 0 || len(m.recentActions) != 0 {
		t.Fatal("Ctrl+Right did not remain word-forward navigation")
	}
}

func TestEditorShortcutHelpAndExplicitRemaps(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	found := false
	for _, item := range m.list.Items {
		if item.ID == "editor" {
			found = item.Detail == "Ctrl+G"
		}
	}
	if !found {
		t.Fatal("command palette did not advertise Ctrl+G for the editor")
	}
	m.openHelp()
	if !strings.Contains(m.infoContent, "Ctrl+G") || !strings.Contains(m.infoContent, "Edit draft in external editor") {
		t.Fatal("help did not advertise the new editor shortcut")
	}
	m.opts.Settings.Keybindings = map[string]string{"editor": "alt+e"}
	if m.actionFor("alt+e") != "editor" || m.actionFor("ctrl+g") != "" || m.binding("editor") != "Alt+E" {
		t.Fatal("new default overrode an explicit editor remap")
	}
	// Existing deliberate Ctrl+E remaps remain user choices, not disposable state.
	m.opts.Settings.Keybindings["editor"] = "ctrl+e"
	if m.actionFor("ctrl+e") != "editor" || m.actionFor("ctrl+g") != "" {
		t.Fatal("existing explicit Ctrl+E editor binding was not preserved")
	}
}
