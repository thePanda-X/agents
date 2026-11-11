package tui

import (
	"fmt"
	"image/color"
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
	v.WindowTitle = m.windowTitle()
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

func (m *Model) windowTitle() string {
	c := m.state.Active()
	status := ""
	if c != nil {
		switch {
		case c.Cancelling:
			status = "Cancelling"
		case c.Loading:
			status = "Loading"
		case c.Busy:
			status = "Responding"
		}
	}
	if status == "" && m.state.Connection == "disconnected" {
		status = "Disconnected"
	}
	var parts []string
	if status != "" {
		parts = append(parts, status)
	}
	if c != nil {
		if title := strings.Join(strings.Fields(theme.Clean(c.Info.Title)), " "); title != "" {
			parts = append(parts, title)
		}
	}
	parts = append(parts, "Agents")
	return strings.Join(parts, " · ")
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
			left = theme.Accent.Render(frame) + theme.Quiet.Render(" Responding… "+elapsedTime(time.Since(c.StartedAt)))
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
	// Keep four columns free so theme.Line can show both sides instead of
	// truncating the right-hand status when the terminal is narrow.
	barWidth := width - 4
	if right != "" {
		barWidth -= lipgloss.Width(right) + 2
	}
	if bar := m.limitBarAt(time.Now(), barWidth); bar != "" {
		if right != "" {
			right += "  "
		}
		right += bar
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
	frame := lipgloss.NewStyle().
		Background(theme.Surface).
		Border(lipgloss.RoundedBorder(), true, true, false, true).
		BorderForeground(border).
		Padding(0, 1).
		Width(width)
	return frame.Render(m.input.View()) + "\n" + composerBottom(width, m.composerMeta(), border)
}

// composerMeta labels the composer's bottom border with the advertised model,
// effort, and mode. Values are kept instead of option names so the frame stays
// compact, mirroring how agent TUIs summarize the active configuration.
func (m *Model) composerMeta() string {
	c := m.state.Active()
	if c == nil {
		return ""
	}
	var models, effort, modes []string
	for _, option := range c.Options {
		value := fmt.Sprint(option.Value)
		for _, choice := range option.Choices {
			if choice.Value == option.Value {
				value = choice.Name
				break
			}
		}
		value = theme.Clean(value)
		if value == "" {
			continue
		}
		switch option.Category {
		case "model", "model_config":
			models = append(models, value)
		case "thought_level":
			effort = append(effort, value)
		case "mode":
			modes = append(modes, value)
		}
	}
	label := strings.Join(models, " · ")
	if value := strings.Join(effort, " · "); value != "" {
		if label == "" {
			label = value
		} else {
			label += " (" + value + ")"
		}
	}
	if value := strings.Join(modes, " · "); value != "" {
		if label != "" {
			label += " · "
		}
		label += value
	}
	return label
}

// composerBottom draws the frame's bottom edge with an optional right-aligned
// label. Lipgloss borders cannot contain text, so the line is composed here and
// measured in cells to keep the composer exactly width columns wide.
func composerBottom(width int, label string, color color.Color) string {
	border := lipgloss.RoundedBorder()
	inner := max(0, width-2)
	lineStyle := lipgloss.NewStyle().Foreground(color).Background(theme.Surface)
	labelStyle := theme.Quiet.Background(theme.Surface)
	text := ""
	if label != "" && width >= 40 {
		text = ansi.Truncate(" "+label+" ", max(0, inner-1), "…")
	}
	if text == "" {
		return lineStyle.Render(border.BottomLeft + strings.Repeat(border.Bottom, inner) + border.BottomRight)
	}
	fill := max(0, inner-lipgloss.Width(text))
	return lineStyle.Render(border.BottomLeft+strings.Repeat(border.Bottom, fill)) +
		labelStyle.Render(text) + lineStyle.Render(border.BottomRight)
}

func (m *Model) footer(width int) string {
	mode := "COMPOSE"
	enter := "Enter:send"
	if name, _, ok := parseSlashInvocation(m.input.Value()); ok && name != "" {
		if local, found := findLocalCommand(name); found && local.Name != "agent" {
			enter = "Enter:run local"
		} else {
			enter = "Enter:send to " + theme.Clean(m.opts.Descriptor.Name)
		}
	}
	hints := []string{enter, m.binding("palette") + ":commands", "Ctrl+J:newline", "Tab:browse"}
	if m.focusedTranscript {
		mode = "BROWSE"
		hints = []string{"j/k:scroll", "Enter:fold", "↑↓:entries", "?:help"}
	}
	if width < 70 {
		hints = []string{enter, "Tab:browse"}
		if m.focusedTranscript {
			hints = []string{"j/k:scroll", "i:compose"}
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
			left += theme.Quiet.Render(fmt.Sprintf(" %d/%d", m.selected+1, len(c.Entries)))
		}
	}
	if m.opts.Settings.Compact {
		hints = []string{m.binding("palette") + ":commands"}
		if m.focusedTranscript {
			hints = []string{"?:help"}
		}
	}
	if width < 40 {
		hints = []string{enter, "Tab"}
		if m.focusedTranscript {
			hints = []string{"j/k:scroll", "i:compose"}
		}
	}
	buffer := m.drafts[m.draftKey()]
	if buffer != nil && len(buffer.Pastes) > 0 && !m.focusedTranscript && m.overlay == "" && m.list == nil && m.interaction == nil {
		hints = []string{enter, m.binding("pastes") + ":pastes", m.binding("editor") + ":edit"}
		if width < 70 {
			hints = []string{enter, m.binding("pastes") + ":pastes"}
		}
		if width < 40 {
			hints = []string{m.binding("pastes") + ":pastes"}
		}
	}
	if m.hasTextSelection() && m.overlay == "" && m.list == nil && m.interaction == nil {
		left = theme.Accent.Bold(true).Render("SELECT")
		hints = []string{"y:copy", "Esc:clear"}
	}
	return footerLine(left, hints, width)
}

// footerLine joins the mode chip and key:action hints, dropping trailing hints
// that do not fit instead of truncating a segment in half.
func footerLine(left string, hints []string, width int) string {
	line := left
	for _, hint := range hints {
		next := line + theme.Quiet.Render(" │ ") + theme.Quiet.Render(hint)
		if lipgloss.Width(next) > width {
			break
		}
		line = next
	}
	return ansi.Truncate(line, width, "…")
}
