package tui

import (
	"strings"
	"testing"

	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestComposerBorderShowsAdvertisedConfiguration(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.state.Active().Options = []harness.ConfigOption{
		{Name: "Model", Category: "model", Value: "sonnet", Choices: []harness.Choice{{Name: "Claude Sonnet", Value: "sonnet"}, {Name: "Other", Value: "other"}}},
		{Name: "Thinking", Category: "thought_level", Value: "high", Choices: []harness.Choice{{Name: "High", Value: "high"}}},
		{Name: "Mode", Category: "mode", Value: "plan", Choices: []harness.Choice{{Name: "Plan", Value: "plan"}}},
	}
	width := m.width - 4
	composer := m.composer(width)
	if !strings.Contains(ansi.Strip(composer), "Claude Sonnet (High) · Plan") {
		t.Fatalf("composer border lost its configuration label: %q", ansi.Strip(composer))
	}
	for _, line := range strings.Split(composer, "\n") {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("composer row is %d cells, want %d: %q", got, width, ansi.Strip(line))
		}
	}
}

func TestComposerHidesConfigurationOnNarrowTerminals(t *testing.T) {
	m := testModel(t)
	m.state.Active().Options = []harness.ConfigOption{
		{Name: "Model", Category: "model", Value: "sonnet", Choices: []harness.Choice{{Name: "Claude Sonnet", Value: "sonnet"}}},
	}
	composer := m.composer(36)
	if strings.Contains(ansi.Strip(composer), "Claude Sonnet") {
		t.Fatalf("narrow composer kept a label that cannot fit: %q", ansi.Strip(composer))
	}
	for _, line := range strings.Split(composer, "\n") {
		if got := lipgloss.Width(line); got != 36 {
			t.Fatalf("narrow composer row is %d cells, want 36: %q", got, ansi.Strip(line))
		}
	}
}

func TestFooterUsesKeyActionSegmentsAndRemappedBindings(t *testing.T) {
	m := testModel(t)
	m.opts.Settings.Keybindings = map[string]string{"palette": "ctrl+k"}
	footer := ansi.Strip(m.footer(120))
	for _, want := range []string{"COMPOSE", "Enter:send", "Ctrl+K:commands", "Ctrl+J:newline", "Tab:browse", " │ "} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer lost %q: %s", want, footer)
		}
	}
	if strings.Contains(footer, "Ctrl+P:commands") {
		t.Fatalf("footer ignored the remapped palette binding: %s", footer)
	}
}

func TestWindowTitleTracksStatusAndSession(t *testing.T) {
	m := testModel(t)
	m.state.Active().Info.Title = "Fix parser"
	if got := m.windowTitle(); got != "Fix parser · Agents" {
		t.Fatalf("idle title = %q", got)
	}
	m.state.Active().Info.Title = "Fix\nparser"
	if got := m.windowTitle(); got != "Fix parser · Agents" {
		t.Fatalf("title with embedded newline = %q", got)
	}
	m.state.Active().Info.Title = "Fix parser"
	m.state.Active().Busy = true
	if got := m.windowTitle(); got != "Responding · Fix parser · Agents" {
		t.Fatalf("busy title = %q", got)
	}
	m.state.Active().Busy = false
	m.state.Apply(harness.Event{Kind: harness.Disconnected, Generation: m.state.Generation})
	if got := m.windowTitle(); got != "Disconnected · Fix parser · Agents" {
		t.Fatalf("disconnected title = %q", got)
	}
}
