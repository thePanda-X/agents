package tui

import (
	"strings"

	"agents/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

// agentsBanner is the launch-state wordmark shown on the fresh-session screen.
// It is rendered from the canonical figlet standard font (uppercase AGENTS).
const agentsBanner = `
   ░███      ░██████  ░██████████ ░███    ░██ ░██████████  ░██████
  ░██░██    ░██   ░██ ░██         ░████   ░██     ░██     ░██   ░██
 ░██  ░██  ░██        ░██         ░██░██  ░██     ░██    ░██
░█████████ ░██  █████ ░█████████  ░██ ░██ ░██     ░██     ░████████
░██    ░██ ░██     ██ ░██         ░██  ░██░██     ░██            ░██
░██    ░██  ░██  ░███ ░██         ░██   ░████     ░██     ░██   ░██
░██    ░██   ░█████░█ ░██████████ ░██    ░███     ░██      ░██████




`

// renderBanner returns the launch-state wordmark styled in the accent color,
// along with whether it fits the available transcript width. Callers gate on
// vertical room separately; the banner is five rows tall.
func renderBanner(width int) (string, bool) {
	need := 0
	for _, line := range strings.Split(agentsBanner, "\n") {
		need = max(need, ansi.StringWidth(line))
	}
	if width < need {
		return "", false
	}
	return theme.Accent.Render(agentsBanner), true
}
