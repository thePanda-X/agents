package tui

import (
	"encoding/json"
	"testing"

	"agents/internal/harness"
	tea "charm.land/bubbletea/v2"
)

func openQuestion(t *testing.T, schema string) (*Model, *harness.Interaction) {
	t.Helper()
	m := testModel(t)
	m.input.SetValue("my unsent draft")
	m.attachments = []harness.Attachment{{}}
	i := &harness.Interaction{ID: "question", Kind: "form", Message: "Choose an approach", Schema: json.RawMessage(schema), Reply: make(chan harness.Decision, 1)}
	m.state.Interactions = []*harness.Interaction{i}
	m.syncInteraction()
	if m.form == nil || m.form.Message != i.Message {
		t.Fatal("question not opened")
	}
	return m, i
}

func TestQuestionSendSkipCancelPreserveDraft(t *testing.T) {
	schema := `{"type":"object","properties":{"q":{"type":"string","enum":["safe","fast"]}},"required":["q"]}`
	for _, action := range []string{"accept", "decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			m, i := openQuestion(t, schema)
			switch action {
			case "accept":
				m.interactionKey(tea.KeyPressMsg{Code: tea.KeySpace})
				m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			case "decline":
				m.interactionKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModAlt})
			case "cancel":
				m.Update(ctrl('c'))
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

func TestQuestionEscSkipsFromOptionsAndLeavesOtherFirst(t *testing.T) {
	m, i := openQuestion(t, `{"type":"object","properties":{"q":{"type":"string","enum":["safe","fast"]},"q_custom":{"type":"string"}}}`)
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.form == nil || m.form.Questions[0].Field.Cursor != len(m.form.Questions[0].Field.Options) {
		t.Fatal("down did not reach Other")
	}
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.form == nil || m.form.Questions[0].Field.Cursor == len(m.form.Questions[0].Field.Options) {
		t.Fatal("Esc in Other did not return to the options")
	}
	select {
	case answer := <-i.Reply:
		t.Fatalf("Esc in Other answered the form: %s", answer.Action)
	default:
	}
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if answer := <-i.Reply; answer.Action != "decline" {
		t.Fatalf("Esc at options did not skip: %s", answer.Action)
	}
}

func TestMultiQuestionReviewSubmitsLabelsAndAnswers(t *testing.T) {
	m, i := openQuestion(t, `{"type":"object","properties":{"q0":{"type":"string","title":"Approach","enum":["safe","fast"]},"q1":{"type":"array","title":"Checks","items":{"type":"string","enum":["unit","race"]}}}}`)
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeySpace})
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form == nil || m.form.Cursor != 1 {
		t.Fatal("first answer did not advance")
	}
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeySpace})
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form == nil || !m.form.Review {
		t.Fatal("last answer did not open the review")
	}
	select {
	case <-i.Reply:
		t.Fatal("sent before review confirmation")
	default:
	}
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	answer := <-i.Reply
	if answer.Action != "accept" || answer.Content["q0"] != "safe" {
		t.Fatalf("wrong answer: %#v", answer)
	}
	if values, ok := answer.Content["q1"].([]string); !ok || len(values) != 1 || values[0] != "unit" {
		t.Fatalf("wrong multi-select answer: %#v", answer.Content["q1"])
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
