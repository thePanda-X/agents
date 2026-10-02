package tui

import (
	"encoding/json"
	"testing"

	"agents/internal/harness"
	tea "charm.land/bubbletea/v2"
)

func TestQuestionReviewSkipCancelPreserveDraft(t *testing.T) {
	for _, action := range []string{"accept", "decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			m := testModel(t)
			m.input.SetValue("my unsent draft")
			m.attachments = []harness.Attachment{{}}
			i := &harness.Interaction{ID: "question", Kind: "form", Message: "Choose an approach", Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","enum":["safe","fast"]}},"required":["q"]}`), Reply: make(chan harness.Decision, 1)}
			m.state.Interactions = []*harness.Interaction{i}
			m.syncInteraction()
			if m.form == nil || m.form.Message != i.Message {
				t.Fatal("question not opened")
			}
			if action == "accept" {
				m.interactionKey(tea.KeyPressMsg{Code: tea.KeySpace})
				m.interactionKey(ctrl('s'))
				if !m.form.Review {
					t.Fatal("missing review")
				}
				select {
				case <-i.Reply:
					t.Fatal("sent before review confirmation")
				default:
				}
				m.interactionKey(tea.KeyPressMsg{Code: tea.KeyTab})
				if m.form.Review {
					t.Fatal("cannot return to editing")
				}
				m.interactionKey(ctrl('s'))
				m.interactionKey(ctrl('s'))
			} else if action == "decline" {
				m.interactionKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModAlt})
			} else {
				m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			}
			select {
			case answer := <-i.Reply:
				if answer.Action != action {
					t.Fatalf("got %s", answer.Action)
				}
				if action == "accept" && answer.Content["q"] != "safe" {
					t.Fatal(answer.Content)
				}
			default:
				t.Fatal("missing answer")
			}
			m.answer(i, harness.Decision{Action: "accept"})
			select {
			case <-i.Reply:
				t.Fatal("duplicate answer")
			default:
			}
			if m.input.Value() != "my unsent draft" || len(m.attachments) != 1 || m.interaction != nil || len(m.state.Interactions) != 0 {
				t.Fatal("draft or interaction lifecycle damaged")
			}
		})
	}
}

func TestQuestionRemoteCancellationAndInvalidSchema(t *testing.T) {
	m := testModel(t)
	done := make(chan struct{})
	i := &harness.Interaction{ID: "q", Kind: "form", Schema: json.RawMessage(`{"type":"object","properties":{}}`), Done: done, Reply: make(chan harness.Decision, 1)}
	m.state.Interactions = []*harness.Interaction{i}
	m.syncInteraction()
	close(done)
	m.answer(i, harness.Decision{Action: "accept"})
	if (<-i.Reply).Action != "cancel" {
		t.Fatal("answered expired request")
	}
	i = &harness.Interaction{ID: "bad", Kind: "form", Schema: json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object"}}}`), Reply: make(chan harness.Decision, 1)}
	m.state.Interactions = []*harness.Interaction{i}
	m.syncInteraction()
	if (<-i.Reply).Action != "cancel" || m.state.LastError == nil {
		t.Fatal("unsupported schema silently skipped")
	}
}
