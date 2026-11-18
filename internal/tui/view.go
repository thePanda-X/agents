package tui

import (
	"fmt"
	"image/color"
	"os"
	"runtime"
	"slices"
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
		v.AltScreen = true
		if m.opts.Settings.Appearance != "terminal" {
			m.setTerminalColors(&v, false)
			v.Content = lipgloss.NewStyle().Background(theme.Background).Foreground(theme.Text).Width(max(1, m.width)).Height(max(1, m.height)).MaxWidth(max(1, m.width)).MaxHeight(max(1, m.height)).Render(theme.FillBackground(v.Content))
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
		canvas.Compose(lipgloss.NewCompositor(lipgloss.NewLayer(m.dimmed(base)), lipgloss.NewLayer(panel).X(max(0, (m.width-lipgloss.Width(panel))/2)).Y(max(0, (m.height-lipgloss.Height(panel))/2)).Z(1)))
		base = canvas.Render()
	}
	v := tea.NewView(base)
	v.AltScreen = true
	v.WindowTitle = m.windowTitle()
	if m.opts.Settings.Appearance == "terminal" {
		v.Content = theme.TerminalDefault(v.Content)
	} else {
		m.setTerminalColors(&v, true)
		v.Content = theme.FillBackground(v.Content)
		if m.opts.Settings.Appearance == "high-contrast" {
			v.Content = theme.HighContrast(v.Content)
		}
	}
	if m.opts.Settings.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// dimmed recedes the interface behind a dialog. Text fragments left visible
// beside a centered panel otherwise read as rendering artifacts, and a uniform
// low-contrast layer keeps attention on the dialog. Terminal default
// appearance has no application colors, so it uses the faint attribute.
func (m *Model) dimmed(content string) string {
	style := lipgloss.NewStyle().Foreground(theme.Border).Background(theme.Background)
	if m.opts.Settings.Appearance == "terminal" {
		style = lipgloss.NewStyle().Faint(true)
	}
	lines := strings.Split(ansi.Strip(content), "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}

// setTerminalDefaultColors controls whether the view changes the terminal's
// default colors (OSC 10/11). Bubble Tea restores them with OSC 110/111 on exit
// and before external process handoffs, but Windows consoles do not reliably
// honor those resets and keep the application background afterwards. Every
// cell is painted explicitly (theme.FillBackground), so the defaults are only
// a cosmetic extra for terminal margins and are skipped on Windows.
var setTerminalDefaultColors = runtime.GOOS != "windows"

func (m *Model) setTerminalColors(v *tea.View, foreground bool) {
	if !setTerminalDefaultColors {
		return
	}
	v.BackgroundColor = theme.Background
	if foreground {
		v.ForegroundColor = theme.Text
	}
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
		if title := strings.Join(strings.Fields(theme.Clean(m.activeSessionLabel())), " "); title != "" {
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
	if label := m.activeSessionLabel(); label != "" {
		left += theme.Quiet.Render(" · " + theme.Clean(label))
	}
	right := theme.Accent.Render(m.opts.Descriptor.Name)
	if c := m.state.Active(); c != nil && c.Usage != nil {
		right += "  " + contextUsage(c.Usage.Used, c.Usage.Size)
		if c.Usage.Cost != nil {
			right += theme.Quiet.Render(fmt.Sprintf(" · %.3f %s", c.Usage.Cost.Amount, c.Usage.Cost.Currency))
		}
	}
	return theme.Line(left, right, width)
}

// contextUsage labels the reported context window and its fill percentage.
// The percentage turns amber above 80% and red above 95% so an approaching
// limit is visible without opening any dialog.
func contextUsage(used, size uint64) string {
	if size == 0 {
		return theme.Quiet.Render("ctx " + tokens(used))
	}
	percent := min(100, int(used*100/size))
	style := theme.Quiet
	switch {
	case percent >= 95:
		style = theme.Error
	case percent >= 80:
		style = theme.Warning
	}
	return theme.Quiet.Render("ctx "+tokens(used)+"/"+tokens(size)+" ") + style.Render(fmt.Sprintf("%d%%", percent))
}

// tokens formats a token count compactly, omitting a trailing ".0" so round
// window sizes read as "200k" rather than "200.0k".
func tokens(n uint64) string {
	format := func(value float64, unit string) string {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0") + unit
	}
	if n >= 1000000 {
		return format(float64(n)/1000000, "M")
	}
	if n >= 1000 {
		return format(float64(n)/1000, "k")
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
			status = "Start typing, or pick a model first with /model."
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
	text := "\n\n" + theme.Body.Render(lipgloss.Wrap(status, m.viewport.Width(), " ")) + "\n\n" + theme.Quiet.Render(lipgloss.Wrap(guidance, m.viewport.Width(), " "))
	intro := theme.Heading.Render("agents")
	// The ASCII wordmark belongs to the fresh-session landing screen: show it
	// only when the harness is ready and the wordmark plus the status and
	// guidance below it fit without clipping. Guidance always wins over art.
	if banner, ok := renderBanner(m.viewport.Width()); ok && m.state.Connection == "ready" && bannerHeight()+lipgloss.Height(text)-1 <= m.viewport.Height() {
		intro = banner
	}
	intro += text
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
			status := " Responding…"
			// A turn adopted without a local start time (for example after a
			// reload) has no meaningful elapsed time to show.
			if !c.StartedAt.IsZero() {
				status += " " + elapsedTime(time.Since(c.StartedAt))
			}
			left = theme.Accent.Render(frame) + theme.Quiet.Render(status)
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
// compact, mirroring how agent TUIs summarize the active configuration. The
// returned label is styled for the composer surface; a mode that lets the agent
// act without asking is shown in the warning color.
func (m *Model) composerMeta() string {
	c := m.state.Active()
	if c == nil {
		return ""
	}
	quiet, warning := theme.Quiet.Background(theme.Surface), theme.Warning.Background(theme.Surface)
	var models, effort, modes []string
	permissive := false
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
			if m.permissiveMode(fmt.Sprint(option.Value), value) {
				permissive = true
			}
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
	styled := quiet.Render(label)
	if value := strings.Join(modes, " · "); value != "" {
		if label != "" {
			styled += quiet.Render(" · ")
		}
		if permissive {
			styled += warning.Render("⚠ " + value)
		} else {
			styled += quiet.Render(value)
		}
	}
	if ansi.Strip(styled) == "" {
		return ""
	}
	return styled
}

// permissiveMode reports whether a mode lets the agent act without asking:
// either declared by the harness descriptor, or (for custom agents) named in a
// way that unambiguously says so.
func (m *Model) permissiveMode(id, name string) bool {
	if slices.Contains(m.opts.Descriptor.PermissiveModes, id) {
		return true
	}
	text := strings.ToLower(id + " " + name)
	for _, word := range []string{"bypass", "yolo", "dangerous", "full-access", "full access"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// composerBottom draws the frame's bottom edge with an optional right-aligned
// label already styled for the composer surface. Lipgloss borders cannot
// contain text, so the line is composed here and measured in cells to keep the
// composer exactly width columns wide.
func composerBottom(width int, label string, color color.Color) string {
	border := lipgloss.RoundedBorder()
	inner := max(0, width-2)
	lineStyle := lipgloss.NewStyle().Foreground(color).Background(theme.Surface)
	pad := theme.Quiet.Background(theme.Surface).Render(" ")
	text := ""
	if label != "" && width >= 40 {
		text = ansi.Truncate(pad+label+pad, max(0, inner-1), "…")
	}
	if text == "" {
		return lineStyle.Render(border.BottomLeft + strings.Repeat(border.Bottom, inner) + border.BottomRight)
	}
	fill := max(0, inner-lipgloss.Width(text))
	return lineStyle.Render(border.BottomLeft+strings.Repeat(border.Bottom, fill)) + text + lineStyle.Render(border.BottomRight)
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
	if width < 40 {
		hints = []string{enter, "Tab"}
		if m.focusedTranscript {
			hints = []string{"j/k:scroll", "i:compose"}
		}
	}
	// Dialogs own the keyboard: describe their keys rather than the composer's.
	overlayOpen := true
	switch {
	case m.interaction != nil:
		mode, hints = "ANSWER", []string{"Enter:confirm", "Ctrl+C:cancel"}
	case m.form != nil:
		mode, hints = "FORM", []string{"Enter:save", "Esc:cancel"}
	case m.list != nil && m.overlay == "search":
		mode, hints = "SEARCH", []string{"Enter:jump", "Esc:close"}
	case m.list != nil:
		mode, hints = "PICKER", m.pickerHints()
	case m.overlay == "info":
		mode, hints = "VIEW", []string{m.escHint()}
		if m.info.TotalLineCount() > m.info.Height() {
			hints = []string{"j/k:scroll", m.escHint()}
		}
	default:
		overlayOpen = false
	}
	left := theme.Accent.Bold(true).Render(mode)
	if m.focusedTranscript && !overlayOpen {
		if c := m.state.Active(); c != nil && len(c.Entries) > 0 {
			left += theme.Quiet.Render(fmt.Sprintf(" %d/%d", m.selected+1, len(c.Entries)))
		}
	}
	if overlayOpen {
		return footerLine(left, hints, width)
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

// escHint says whether Esc returns to a parent picker or closes the dialog.
func (m *Model) escHint() string {
	if len(m.overlayStack) > 0 {
		return "Esc:back"
	}
	return "Esc:close"
}

// pickerHints names what Enter actually does in the open picker, plus its
// picker-specific keys, so the footer matches the dialog's own instructions.
func (m *Model) pickerHints() []string {
	enter := "Enter:choose"
	var extra []string
	switch m.overlay {
	case "settings":
		enter = "Enter:toggle"
	case "commands":
		enter = "Enter:insert"
	case "files":
		enter, extra = "Enter:attach", []string{"Tab:mark"}
	case "attachments", "pastes":
		enter, extra = "Enter:inspect", []string{"Del:remove"}
	case "sessions":
		enter, extra = "Enter:load", []string{"Ctrl+R:rename"}
		if m.state.Peer.Capabilities.Delete {
			extra = append(extra, "Ctrl+X:delete")
		}
	case "history":
		enter = "Enter:use"
	case "code":
		enter = "Enter:copy"
	case "palette":
		enter = "Enter:run"
	case "changes", "file_changes":
		enter = "Enter:inspect"
	case "harnesses":
		enter = "Enter:switch"
	case "auth":
		enter = "Enter:sign in"
	case "skills":
		enter = "Enter:stage"
	case "delete":
		enter = "Enter:confirm"
	}
	if m.list != nil && m.list.Error != "" {
		enter, extra = "Enter:retry", nil
	}
	// Enter and Esc come first so narrow footers drop the optional keys.
	return append(append([]string{enter, m.escHint()}, extra...), "↑↓:select")
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
