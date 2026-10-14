package tui

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"

	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
)

type linkOpenedMsg struct{ err error }

func (m *Model) linkAt(mouse tea.Mouse) string {
	if !m.inTranscript(mouse) {
		return ""
	}
	x, y := m.transcriptOrigin()
	row := m.viewport.YOffset() + mouse.Y - y
	lines := strings.Split(m.transcriptContent, "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}
	return transcript.LinkAt(lines[row], m.viewport.XOffset()+mouse.X-x)
}

func openLink(ctx context.Context, target string) tea.Cmd {
	return func() tea.Msg {
		if err := validateWebURL(target); err != nil {
			return linkOpenedMsg{err}
		}
		return linkOpenedMsg{launchBrowser(ctx, target)}
	}
}

func validateWebURL(target string) error {
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" || u.User != nil ||
		(u.Scheme != "https" && u.Scheme != "http") ||
		strings.ContainsFunc(target, unicode.IsControl) {
		return fmt.Errorf("links must use HTTP or HTTPS, without embedded credentials or control characters")
	}
	return nil
}

// Launch on the machine running Agents, without interpreting the URL as shell
// source. Both explicitly clicked links and consented ACP interactions use this.
func launchBrowser(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", target)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", target)
	}
	return cmd.Run()
}
