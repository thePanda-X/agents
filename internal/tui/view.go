package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"agents/internal/tui/theme"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) View() tea.View {
	if m.width < 32 || m.height < 12 {
		v := tea.NewView(lipgloss.NewStyle().Width(max(1, m.width)).MaxHeight(max(1, m.height)).Render("agents\nResize terminal (32×12 minimum).\n" + m.binding("quit") + " quit"))
		v.AltScreen, v.BackgroundColor = true, theme.Background
		if m.opts.Settings.Appearance == "terminal" {
			v.BackgroundColor = nil
		}
		return v
	}
	width := max(8, m.width-4)
	parts := []string{m.header(width), "", m.conversationView(), m.feedback(width), m.activity(width)}
	if m.opts.Settings.Compact {
		parts = []string{m.header(width), m.conversationView(), m.feedback(width), m.activity(width)}
	}
	if len(m.attachments) > 0 {
		parts = append(parts, m.attachmentView())
	}
	parts = append(parts, m.composer(width))
	if !m.opts.Settings.Compact {
		parts = append(parts, m.configLine(width))
	}
	parts = append(parts, m.footer(width))
	content := strings.Join(parts, "\n")
	base := lipgloss.NewStyle().Background(theme.Background).Foreground(theme.Text).Width(m.width).Height(m.height).MaxWidth(m.width).MaxHeight(m.height).Padding(0, 2).Render(content)
	if m.list != nil || m.form != nil || m.overlay == "info" {
		style, panelWidth, panelHeight := m.modalLayout()
		panel := style.Render(m.modalContent(panelWidth, panelHeight))
		canvas := lipgloss.NewCanvas(m.width, m.height)
		// Layer.Draw does not apply Layer.X/Y. The compositor resolves offsets.
		canvas.Compose(lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(panel).X(max(0, (m.width-lipgloss.Width(panel))/2)).Y(max(0, (m.height-lipgloss.Height(panel))/2)).Z(1)))
		base = canvas.Render()
	}
	v := tea.NewView(base)
	v.AltScreen, v.BackgroundColor, v.ForegroundColor = true, theme.Background, theme.Text
	v.WindowTitle = "Agents · " + theme.Clean(m.opts.Descriptor.Name)
	if m.opts.Settings.Appearance == "terminal" {
		v.Content = theme.TerminalDefault(v.Content)
		v.BackgroundColor, v.ForegroundColor = nil, nil
	} else if m.opts.Settings.Appearance == "high-contrast" {
		v.Content = theme.HighContrast(v.Content)
	}
	if m.opts.Settings.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m *Model) header(width int) string {
	workspace := m.activeWorkspace()
	branch := ""
	if workspace == m.branchWorkspace {
		branch = m.branch
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(workspace, home+string(os.PathSeparator)) {
		workspace = "~" + strings.TrimPrefix(workspace, home)
	}
	if branch != "" {
		workspace += ":" + branch
	}
	left := theme.Quiet.Render(theme.Clean(workspace))
	if c := m.state.Active(); c != nil && c.Info.Title != "" {
		left += theme.Quiet.Render(" · " + theme.Clean(c.Info.Title))
	}
	right := theme.Accent.Render(m.opts.Descriptor.Name)
	if c := m.state.Active(); c != nil && c.Usage != nil {
		right += theme.Quiet.Render(fmt.Sprintf("  %s / %s", tokens(c.Usage.Used), tokens(c.Usage.Size)))
		if c.Usage.Cost != nil {
			right += theme.Quiet.Render(fmt.Sprintf(" · %.3f %s", c.Usage.Cost.Amount, c.Usage.Cost.Currency))
		}
	}
	return theme.Line(left, right, width)
}

func tokens(n uint64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

func elapsedTime(elapsed time.Duration) string {
	seconds := max(int64(0), int64(elapsed/time.Second))
	var parts []string
	if hours := seconds / 3600; hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes := seconds / 60 % 60; minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if remainder := seconds % 60; remainder > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", remainder))
	}
	return strings.Join(parts, " ")
}

func (m *Model) conversationView() string {
	if c := m.state.Active(); c != nil && len(c.Entries) > 0 {
		return m.viewport.View()
	}
	status := "Connecting to " + m.opts.Descriptor.Name + "…"
	if m.state.Connection == "ready" {
		status = "Type a task and press Enter to start."
		if m.state.Active() == nil {
			status = "Type a task and press Enter. /model prepares a session before your first task."
		}
	}
	if m.state.Connection == "disconnected" {
		status = "Agent disconnected. " + m.binding("reconnect") + " to reconnect."
	}
	guidance := m.binding("sessions") + " resume conversation · /harness choose agent\n" + m.binding("options") + " model & effort · " + m.binding("files") + " attach file\nTab browse · Ctrl+J newline · " + m.binding("palette") + " commands"
	if m.hasWorked {
		guidance = m.binding("sessions") + " resume · " + m.binding("palette") + " commands · /harness choose agent"
	}
	if m.state.Connection == "disconnected" {
		guidance = "/diagnostics setup details · /auth sign in\n" + m.binding("reconnect") + " retry connection · /harness choose agent"
	}
	if m.viewport.Height() < 8 {
		guidance = m.binding("sessions") + " resume · " + m.binding("palette") + " commands"
		status = "Enter sends a task. Ctrl+J adds a newline."
		if m.state.Connection != "ready" {
			status = "Agent " + m.state.Connection + " · /diagnostics"
		}
	}
	if m.viewport.Height() < 5 {
		return lipgloss.NewStyle().Width(m.viewport.Width()).Height(m.viewport.Height()).MaxHeight(m.viewport.Height()).Render(theme.Quiet.Render(guidance))
	}
	intro := theme.Heading.Render("agents") + "\n\n" + theme.Body.Render(lipgloss.Wrap(status, m.viewport.Width(), " ")) + "\n\n" + theme.Quiet.Render(lipgloss.Wrap(guidance, m.viewport.Width(), " "))
	return lipgloss.NewStyle().Width(m.viewport.Width()).Height(m.viewport.Height()).MaxHeight(m.viewport.Height()).Align(lipgloss.Center, lipgloss.Center).Render(intro)
}

func (m *Model) activity(width int) string {
	left := theme.Quiet.Render("Ready")
	if m.state.Connection != "ready" {
		left = theme.Quiet.Render("Connecting…")
		if m.state.Connection == "disconnected" {
			left = theme.Error.Render("Disconnected")
		}
	} else if m.interaction != nil {
		left = theme.Accent.Render("Waiting for your answer")
	} else if m.sessionPending && m.state.Connection == "ready" {
		left = theme.Quiet.Render("◌ Opening session…")
	} else if c := m.state.Active(); c != nil {
		switch {
		case c.Loading:
			left = theme.Quiet.Render("◌ Loading session history…")
		case c.Cancelling:
			left = theme.Quiet.Render("◌ Cancelling…")
		case c.Busy:
			frames := []string{"·", "✢", "✳", "✶"}
			frame := frames[(time.Now().UnixMilli()/200)%int64(len(frames))]
			if m.opts.Settings.ReducedMotion {
				frame = "●"
			}
			left = theme.Accent.Render(frame) + theme.Quiet.Render(" Working · "+elapsedTime(time.Since(c.StartedAt)))
		}
	}
	right := ""
	if c := m.state.Active(); c != nil && c.Busy {
		right = theme.Quiet.Render("Ctrl+C stop")
	}
	if m.creatingSession && m.pendingFirstPrompt != nil {
		right = theme.Quiet.Render("Ctrl+C cancel first send")
	}
	if m.unseen {
		right = theme.Accent.Render("↓ new · Ctrl+End")
	}
	if m.state.Connection == "disconnected" {
		right = theme.Quiet.Render(m.binding("reconnect") + " reconnect")
	}
	return theme.Line(left, right, width)
}

func (m *Model) attachmentView() string {
	var labels []string
	for _, a := range m.attachments {
		labels = append(labels, "@"+theme.Clean(a.Name))
	}
	return theme.Accent.Render(ansi.Truncate(fmt.Sprintf("%d attached · ", len(labels))+strings.Join(labels, "  ")+" · /attachments", max(8, m.width-4), "…"))
}

func (m *Model) composer(width int) string {
	border := theme.Border
	if !m.focusedTranscript && m.list == nil && m.interaction == nil && m.overlay == "" {
		border = theme.Pink
	}
	return lipgloss.NewStyle().Background(theme.Surface).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(width).Render(m.input.View())
}

func (m *Model) configLine(width int) string {
	var labels []string
	if c := m.state.Active(); c != nil {
		for _, option := range c.Options {
			if option.Category != "model" && option.Category != "thought_level" && option.Category != "mode" && option.Category != "model_config" {
				continue
			}
			name := fmt.Sprint(option.Value)
			for _, choice := range option.Choices {
				if choice.Value == option.Value {
					name = choice.Name
					break
				}
			}
			label := option.Name
			if option.Category == "thought_level" {
				label = "Effort"
			}
			labels = append(labels, theme.Clean(label+": "+name))
		}
	}
	return theme.Quiet.Width(width).Align(lipgloss.Right).Render(ansi.Truncate(strings.Join(labels, " · "), width, "…"))
}

func (m *Model) footer(width int) string {
	mode := "COMPOSE"
	enter := "Enter send"
	if name, _, ok := parseSlashInvocation(m.input.Value()); ok && name != "" {
		if local, found := findLocalCommand(name); found && local.Name != "agent" {
			enter = "Enter run local"
		} else {
			enter = "Enter send to " + theme.Clean(m.opts.Descriptor.Name)
		}
	}
	hints := enter + " · Ctrl+J newline · " + m.binding("palette") + " commands"
	if m.focusedTranscript {
		mode = "BROWSE"
		hints = "j/k scroll · Enter fold · ↑↓ entries · ? help"
	}
	if width < 70 {
		hints = enter + " · Tab browse"
		if m.focusedTranscript {
			hints = "j/k scroll · i compose"
		}
	}
	if m.list != nil {
		mode = "PICKER"
		if m.overlay == "search" {
			mode = "SEARCH"
		}
	}
	left := theme.Accent.Bold(true).Render(mode)
	if m.focusedTranscript {
		if c := m.state.Active(); c != nil && len(c.Entries) > 0 {
			left += theme.Quiet.Render(fmt.Sprintf(" · %d/%d", m.selected+1, len(c.Entries)))
		}
	}
	if m.opts.Settings.Compact {
		hints = m.binding("palette") + " commands"
		if m.focusedTranscript {
			hints = "? help"
		}
		config := strings.TrimSpace(theme.Clean(m.configLine(width)))
		if config != "" {
			left += theme.Quiet.Render(" · " + config)
		}
	}
	if width < 40 {
		hints = "Enter send · Tab"
		if enter == "Enter run local" {
			hints = "Enter local · Tab"
		} else if enter != "Enter send" {
			hints = "Enter agent · Tab"
		}
		if m.focusedTranscript {
			hints = "j/k · i compose"
		}
	}
	return theme.Line(left, theme.Quiet.Render(hints), width)
}
