package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
)

func TestCtrlTCyclesAdvertisedEffortLevelsAndWraps(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Active().Options = []harness.ConfigOption{{ID: "effort", Name: "Effort", Type: "select", Category: "thought_level", Value: "medium", Choices: []harness.Choice{{Value: "low", Name: "Low"}, {Value: "medium", Name: "Medium"}, {Value: "high", Name: "High"}}}}
	draft := "Keep this draft"
	m.input.SetValue(draft)
	visibility := m.opts.Settings.ShowThoughts
	for _, expected := range []string{"high", "low", "medium"} {
		cmd := m.handleKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
		if cmd == nil || !m.effortPending {
			t.Fatal("Ctrl+T did not request effort change")
		}
		if m.handleKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}) != nil {
			t.Fatal("repeated key raced pending option change")
		}
		cmd()
		m.Update(eventMsg(<-m.opts.Controller.Events))
		if m.effortPending || m.state.Active().Options[0].Value != expected {
			t.Fatal("effort did not advance or pending state stuck")
		}
	}
	if !reflect.DeepEqual(adapter.optionValues, []any{"high", "low", "medium"}) || len(adapter.prompts) != 0 || m.input.Value() != draft || m.opts.Settings.ShowThoughts != visibility {
		t.Fatal("effort cycling sent a prompt or changed draft/reasoning visibility")
	}
}

func TestEffortCycleHandlesUnavailableAndChangingOptions(t *testing.T) {
	m, _ := modelWithCommandAdapter(t)
	if m.cycleEffort() != nil || !strings.Contains(m.state.Notice, "does not advertise") {
		t.Fatal("unsupported effort lacked feedback")
	}
	m.newSession()
	if m.cycleEffort() == nil || m.pendingSetup == nil || m.pendingFirstPrompt != nil {
		t.Fatal("effort control did not prepare configuration without a task")
	}
	m.sessionPending, m.creatingSession, m.pendingSetup = false, false, nil
	m.state.ActiveID = "s"
	m.state.Active().Options = []harness.ConfigOption{{ID: "effort", Type: "select", Category: "thought_level", Value: "unknown", Choices: []harness.Choice{{Value: "low"}, {Value: "high"}}}}
	cmd := m.cycleEffort()
	if cmd == nil {
		t.Fatal("unknown current value could not start cycling")
	}
	cmd()
	m.Update(eventMsg(<-m.opts.Controller.Events))
	if m.state.Active().Options[0].Value != "low" {
		t.Fatal("unknown current value did not select first advertised level")
	}
	m.effortPending = true
	m.Update(eventMsg(harness.Event{Kind: harness.Failure, Generation: m.state.Generation, Err: errors.New("rejected")}))
	if m.effortPending {
		t.Fatal("failed change left effort cycling blocked")
	}
	m.state.Active().Options[0].Choices = []harness.Choice{{Value: "low", Name: "Low"}}
	if m.cycleEffort() != nil || !strings.Contains(m.state.Notice, "Only one") {
		t.Fatal("single-level effort should not request a no-op")
	}
}

func TestEffortShortcutCanBeRemappedAndThoughtVisibilityStillWorks(t *testing.T) {
	m := testModel(t)
	m.opts.Settings.Keybindings = map[string]string{"effort": "alt+t", "thoughts": "ctrl+t"}
	if m.actionFor("alt+t") != "effort" || m.actionFor("ctrl+t") != "thoughts" {
		t.Fatal("explicit remapping did not override new default")
	}
	before := m.opts.Settings.ShowThoughts
	m.input.SetValue("/thoughts")
	m.send()
	if m.opts.Settings.ShowThoughts == before {
		t.Fatal("/thoughts no longer toggles reasoning visibility")
	}
}
