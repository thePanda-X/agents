package tui

import (
	"strings"

	"agents/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

// agentsBanner is the launch-state wordmark shown on the fresh-session screen.
// It carries no blank padding rows: the launch view owns vertical spacing so it
// can measure whether the wordmark and the guidance below it both fit.
const agentsBanner = `   ░███      ░██████  ░██████████ ░███    ░██ ░██████████  ░██████
  ░██░██    ░██   ░██ ░██         ░████   ░██     ░██     ░██   ░██
 ░██  ░██  ░██        ░██         ░██░██  ░██     ░██    ░██
░█████████ ░██  █████ ░█████████  ░██ ░██ ░██     ░██     ░████████
░██    ░██ ░██     ██ ░██         ░██  ░██░██     ░██            ░██
░██    ░██  ░██  ░███ ░██         ░██   ░████     ░██     ░██   ░██
░██    ░██   ░█████░█ ░██████████ ░██    ░███     ░██      ░██████`

// renderBanner returns the launch-state wordmark styled in the accent color,
// along with whether it fits the available transcript width. Callers gate on
// vertical room separately using bannerHeight.
func renderBanner(width int) (string, bool) {
	if width < bannerWidth() {
		return "", false
	}
	return theme.Accent.Render(agentsBanner), true
}

func bannerWidth() int {
	need := 0
	for _, line := range strings.Split(agentsBanner, "\n") {
		need = max(need, ansi.StringWidth(line))
	}
	return need
}

func bannerHeight() int { return strings.Count(agentsBanner, "\n") + 1 }
