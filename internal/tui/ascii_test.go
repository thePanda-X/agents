package tui

import (
	"strings"
	"testing"

	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderBannerFitting(t *testing.T) {
	if got, ok := renderBanner(50); !ok || !strings.Contains(got, "|____/") {
		t.Fatalf("renderBanner(50) = (%q, %v), want fit", got, ok)
	}
	if got, ok := renderBanner(49); ok || got != "" {
		t.Fatalf("renderBanner(49) = (%q, %v), want no fit", got, ok)
	}
}

func TestLaunchStateShowsFigletBannerWhenReadyAndFits(t *testing.T) {
	m := newTestModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := ansi.Strip(m.conversationView())
	for _, want := range []string{"|____/", "|_| \\_|", "Type a task and press Enter"} {
		if !strings.Contains(view, want) {
			t.Fatalf("launch view lost %q: %s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.viewport.Width() {
			t.Fatalf("launch line is %d cells, viewport %d: %q", got, m.viewport.Width(), line)
		}
	}
}

func TestLaunchStateHidesBannerWhileConnecting(t *testing.T) {
	m := newTestModel(t, false)
	m.state.Apply(harness.Event{Kind: harness.Connecting, Generation: 2})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := ansi.Strip(m.conversationView())
	if strings.Contains(view, "|____/") {
		t.Fatalf("connecting state showed the banner: %s", view)
	}
	if !strings.Contains(view, "Connecting to Demo") {
		t.Fatalf("connecting state lost its status: %s", view)
	}
}

func TestLaunchStateHidesBannerWhenItDoesNotFit(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {38, 30}} {
		m := newTestModel(t, false)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.conversationView())
		if strings.Contains(view, "|_.__/") {
			t.Fatalf("banner shown at %dx%d: %s", size[0], size[1], view)
		}
	}
}

func TestResponsiveLaunchViewStaysInsideTerminal(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 14}, {24, 8}} {
		m := newTestModel(t, false)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		if width, height := lipgloss.Size(view); width > size[0] || height > size[1] {
			t.Fatalf("launch view %dx%d overflows terminal %dx%d", width, height, size[0], size[1])
		}
	}
}
