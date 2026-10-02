package tui

import (
	"errors"
	"testing"

	"agents/internal/harness"
	tea "charm.land/bubbletea/v2"
)

func setupOptions() []harness.ConfigOption {
	return []harness.ConfigOption{
		{ID: "model", Name: "Model", Category: "model", Type: "select", Value: "small", Choices: []harness.Choice{{Name: "Small", Value: "small"}, {Name: "Large", Value: "large"}}},
		{ID: "effort", Name: "Thinking", Category: "thought_level", Type: "select", Value: "low", Choices: []harness.Choice{{Name: "Low", Value: "low"}, {Name: "High", Value: "high"}}},
	}
}

func TestConfigureBeforeFirstTask(t *testing.T) {
	for _, control := range []string{"options", "model", "effort", "ctrl+t"} {
		t.Run(control, func(t *testing.T) {
			m, adapter := modelWithCommandAdapter(t)
			m.newSession()
			m.input.SetValue("unsent task")
			m.attachments = []harness.Attachment{{Name: "notes.md"}}
			var create tea.Cmd
			switch control {
			case "options":
				create = m.perform("options")
			case "ctrl+t":
				create = m.handleKey(ctrl('t'))
			default:
				m.input.SetValue("/" + control)
				create = m.send()
			}
			if create == nil || m.pendingSetup == nil || m.pendingFirstPrompt != nil {
				t.Fatal("configuration did not prepare empty session")
			}
			if m.openOptions() != nil || m.sendPrompt("do not send") != nil {
				t.Fatal("duplicate creation or task queued while preparing")
			}
			m.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: m.state.Generation - 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "stale"}}}))
			if m.pendingSetup == nil || m.state.Active() != nil {
				t.Fatal("stale ready consumed setup")
			}
			m.input.SetValue("edited unsent task")
			create()
			event := <-m.opts.Controller.Events
			event.Session.Options = setupOptions()
			m.Update(eventMsg(event))
			if adapter.newSessions != 1 || len(adapter.prompts) != 0 || m.input.Value() != "edited unsent task" || len(m.attachments) != 1 {
				t.Fatal("setup sent task or lost draft/attachment")
			}
			if control == "options" {
				if m.overlay != "options" {
					t.Fatal("options picker missing")
				}
				m.choose(m.list.Items[0])
			}
			if m.overlay != "values" || m.list == nil {
				t.Fatal("configuration picker missing")
			}
			change := m.choose(m.list.Items[1])
			if change == nil || !m.configPending || m.sendPrompt("edited unsent task") != nil {
				t.Fatal("task raced configuration")
			}
			change()
			m.Update(eventMsg(<-m.opts.Controller.Events))
			if m.configPending {
				t.Fatal("configuration stuck pending")
			}
			send := m.sendPrompt("edited unsent task")
			if send == nil {
				t.Fatal("task blocked after configuration")
			}
			send()
			if len(adapter.prompts) != 1 || adapter.prompts[0].Text != "edited unsent task" || len(adapter.optionValues) != 1 {
				t.Fatal("task/configuration did not complete")
			}
		})
	}
}

func TestSetupFailuresAndCancellationPreserveDraft(t *testing.T) {
	for _, result := range []string{"failure", "cancel", "unsupported", "reconnect"} {
		t.Run(result, func(t *testing.T) {
			m, adapter := modelWithCommandAdapter(t)
			m.newSession()
			m.input.SetValue("keep task")
			create := m.cycleEffort()
			switch result {
			case "failure":
				m.Update(eventMsg(harness.Event{Kind: harness.Failure, Generation: m.state.Generation, Err: errors.New("create failed")}))
			case "cancel":
				m.handleKey(ctrl('c'))
				create()
				m.Update(eventMsg(<-m.opts.Controller.Events))
			case "unsupported":
				create()
				m.Update(eventMsg(<-m.opts.Controller.Events))
				if m.state.LastError == nil {
					t.Fatal("unsupported effort not explained")
				}
			case "reconnect":
				m.reconnect()
			}
			if m.input.Value() != "keep task" || len(adapter.prompts) != 0 || m.pendingSetup != nil || m.pendingFirstPrompt != nil || m.list != nil {
				t.Fatal("setup failure/cancellation damaged draft or opened stale picker")
			}
		})
	}
}

func TestModelArgumentAppliesBeforeFirstTask(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.newSession()
	m.input.SetValue("/model large")
	create := m.send()
	create()
	event := <-m.opts.Controller.Events
	event.Session.Options = setupOptions()
	_, followup := m.Update(eventMsg(event))
	if m.input.Value() != "" || !m.configPending {
		t.Fatal("model command not consumed or applied")
	}
	followup().(tea.BatchMsg)[0]()
	m.Update(eventMsg(<-m.opts.Controller.Events))
	if len(adapter.optionValues) != 1 || adapter.optionValues[0] != "large" || len(adapter.prompts) != 0 {
		t.Fatal("model argument leaked to task")
	}
}
