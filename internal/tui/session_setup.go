package tui

import (
	"fmt"

	"agents/internal/harness"
	tea "charm.land/bubbletea/v2"
)

// ACP advertises configuration with session/new, not initialize. An explicit
// configuration action prepares a session but must never queue a task.
type sessionSetupRequest struct {
	category, name, args, invocation string
}

func (m *Model) prepareSessionSetup(request sessionSetupRequest) tea.Cmd {
	if m.sessionPending {
		m.state.Notice = "Session preparation pending · please wait"
		return nil
	}
	if m.state.Connection != "ready" {
		m.state.Notice = "Connect to the harness before choosing model or thinking options"
		return nil
	}
	m.pendingSetup = &request
	// An explicit setup request must not trigger the demo's automatic prompt.
	if m.opts.Demo {
		m.demoStarted = true
	}
	m.creatingSession, m.sessionPending = true, true
	m.pendingFirstPrompt = nil
	m.state.Notice = "Preparing an empty session for configuration · no task will be sent"
	controller := m.opts.Controller
	return command(controller.NewSession)
}

func (m *Model) finishSessionSetup(request sessionSetupRequest) tea.Cmd {
	// Consume only the original local command, never edits made during creation.
	if request.invocation != "" && m.input.Value() == request.invocation {
		m.consumeSlashCommand()
	}
	m.state.Notice = "Session prepared · choose your options before sending a task"
	if request.category == "" {
		return m.openOptions()
	}
	if c := m.state.Active(); c != nil {
		for _, option := range c.Options {
			if option.Category != request.category {
				continue
			}
			if request.args == "" {
				m.openOptionValues(option)
				return nil
			}
			value, err := matchOptionChoice(option, request.args)
			if err != nil {
				m.state.LastError = err
				return nil
			}
			return m.setSessionOption(option, value)
		}
	}
	m.state.LastError = fmt.Errorf("this harness/model does not advertise /%s options", request.name)
	return nil
}

func (m *Model) setSessionOption(option harness.ConfigOption, value any) tea.Cmd {
	c := m.state.Active()
	if c == nil || m.configPending || m.sessionPending || c.Loading || m.state.Connection != "ready" {
		m.state.Notice = "Configuration not ready · please wait"
		return nil
	}
	m.configPending = true
	m.state.Notice = "Applying " + option.Name + "…"
	id, controller := c.Info.ID, m.opts.Controller
	return command(func() { controller.SetOption(id, option, value) })
}
