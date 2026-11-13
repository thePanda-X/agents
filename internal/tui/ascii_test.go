package tui

import (
	"strings"
	"testing"

	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// bannerRow is a distinctive fragment of the wordmark used to detect it.
const bannerRow = "░█████████ ░██  █████"

func TestRenderBannerFitting(t *testing.T) {
	width := bannerWidth()
	if got, ok := renderBanner(width); !ok || !strings.Contains(got, bannerRow) {
		t.Fatalf("renderBanner(%d) = (%q, %v), want fit", width, got, ok)
	}
	if got, ok := renderBanner(width - 1); ok || got != "" {
		t.Fatalf("renderBanner(%d) = (%q, %v), want no fit", width-1, got, ok)
	}
}

func TestBannerHasNoBlankPaddingRows(t *testing.T) {
	for _, line := range strings.Split(agentsBanner, "\n") {
		if strings.TrimSpace(line) == "" {
			t.Fatal("banner carries blank rows; the launch view owns spacing")
		}
	}
}

func TestLaunchStateShowsFigletBannerWhenReadyAndFits(t *testing.T) {
	m := newTestModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := ansi.Strip(m.conversationView())
	for _, want := range []string{bannerRow, "Start typing", m.binding("palette") + " commands"} {
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

// Regression: on common 20–24 row terminals the wordmark used to fill the
// viewport and clip every line of guidance below it.
func TestLaunchGuidanceIsNeverClippedByTheBanner(t *testing.T) {
	for _, height := range []int{16, 20, 24, 30} {
		m := newTestModel(t, false)
		m.Update(tea.WindowSizeMsg{Width: 90, Height: height})
		view := ansi.Strip(m.conversationView())
		if !strings.Contains(view, m.binding("palette")+" commands") {
			t.Fatalf("guidance clipped at 90x%d:\n%s", height, view)
		}
	}
}

func TestLaunchStateHidesBannerWhileConnecting(t *testing.T) {
	m := newTestModel(t, false)
	m.state.Apply(harness.Event{Kind: harness.Connecting, Generation: 2})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := ansi.Strip(m.conversationView())
	if strings.Contains(view, bannerRow) {
		t.Fatalf("connecting state showed the banner: %s", view)
	}
	if !strings.Contains(view, "Connecting to Demo") {
		t.Fatalf("connecting state lost its status: %s", view)
	}
}

func TestLaunchStateHidesBannerWhenItDoesNotFit(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {38, 30}, {120, 14}} {
		m := newTestModel(t, false)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.conversationView())
		if strings.Contains(view, bannerRow) {
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
