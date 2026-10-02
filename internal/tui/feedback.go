package tui

import (
	"errors"
	"strings"
	"time"

	"agents/internal/harness"
	"agents/internal/tui/theme"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) trackFeedback() {
	if m.state.Notice != m.noticeText {
		m.noticeText, m.noticeAt = m.state.Notice, time.Now()
	}
	text := ""
	if m.state.LastError != nil {
		text = m.state.LastError.Error()
	}
	if text != m.errorText {
		m.errorText, m.errorAt = text, time.Now()
	}
}

func (m *Model) feedback(width int) string {
	if m.state.LastError != nil && (m.errorAt.IsZero() || time.Since(m.errorAt) < 10*time.Second) && (m.errorAt.IsZero() || !m.noticeAt.After(m.errorAt)) {
		message := strings.Join(strings.Fields(theme.Clean(m.state.LastError.Error())), " ")
		if errors.Is(m.state.LastError, harness.ErrAuthRequired) {
			message = "Authentication required · /auth to sign in"
		}
		hint := "/diagnostics"
		if buffer := m.drafts[m.draftKey()]; buffer != nil && buffer.Failed != nil {
			hint = "/recover"
		}
		return theme.Line(theme.Error.Render("! "+message), theme.Quiet.Render(hint), width)
	}
	if m.state.Notice != "" && (m.noticeAt.IsZero() || time.Since(m.noticeAt) < 8*time.Second) {
		return theme.Quiet.Render(ansi.Truncate(strings.Join(strings.Fields(theme.Clean(m.state.Notice)), " "), width, "…"))
	}
	return ""
}

func (m *Model) openDiagnostics() {
	content := "No error details available."
	if m.state.LastError != nil {
		content = theme.Clean(m.state.LastError.Error())
	}
	content += "\n\nConnection: " + m.state.Connection
	content += "\n\n" + m.binding("reconnect") + " reconnects without resending a prompt."
	if m.opts.Descriptor.LoginHint != "" {
		content += "\n\n" + theme.Clean(m.opts.Descriptor.LoginHint)
	}
	if m.currentDraft().Failed != nil {
		content += "\n\n/recover restores the interrupted submission for review. The agent may already have processed part of it."
	}
	m.openInfo("Connection and error details", content)
}

func (m *Model) retryOverlay() tea.Cmd {
	kind, query := m.overlay, m.list.Search.Value()
	m.state.LastError = nil
	switch kind {
	case "sessions":
		return m.openSessions()
	case "files":
		return m.openFiles(query)
	case "skills":
		return m.openSkills(query)
	}
	return nil
}
