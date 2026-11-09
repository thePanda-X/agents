package overlays

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func pressMsg(form *Form, msg tea.KeyPressMsg) Action {
	action, _ := form.Key(msg)
	return action
}

func press(form *Form, code rune) Action {
	return pressMsg(form, tea.KeyPressMsg{Code: code})
}

func text(form *Form, value string) {
	for _, r := range value {
		form.Key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func questionNamed(form *Form, name string) *Question {
	for _, question := range form.Questions {
		if question.Field.Name == name {
			return question
		}
	}
	return nil
}

func TestFormDefaultsTypedAnswersAndValidation(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","default":"Alex","minLength":2},"count":{"type":"integer","default":2,"minimum":1},"enabled":{"type":"boolean","default":true},"strategy":{"type":"string","enum":["safe","fast"],"default":"safe"}},"required":["name","count","enabled","strategy"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !form.Submit() {
		t.Fatal(form.Error)
	}
	if form.Content["enabled"] != true || form.Content["count"] != int64(2) || form.Content["name"] != "Alex" || form.Content["strategy"] != "safe" {
		t.Fatalf("wrong typed content: %+v", form.Content)
	}
	count := questionNamed(form, "count")
	if count == nil {
		t.Fatal("missing count question")
	}
	count.Field.Input.SetValue("0")
	if form.Submit() || form.Error == "" {
		t.Fatal("invalid minimum accepted")
	}
}

func TestClaudeQuestionsKeepOtherBesideTheirQuestion(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{
		"type":"object",
		"properties":{
			"question_0":{"type":"string","title":"Approach","description":"Which approach should we use?","oneOf":[{"const":"safe","title":"Safe","description":"Small changes","_meta":{"_claude/askUserQuestionOption":{"preview":"Keep the public API stable."}}},{"const":"fast","title":"Fast","description":"Larger changes"}]},
			"question_0_custom":{"type":"string","title":"Other"},
			"question_1":{"type":"array","title":"Checks","description":"Which checks should run?","items":{"anyOf":[{"const":"unit,tests","title":"Unit tests"},{"const":"race","title":"Race tests"}]},"minItems":1,"uniqueItems":true},
			"question_1_custom":{"type":"string","title":"Other"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(form.Questions) != 2 || questionNamed(form, "question_0_custom") != nil {
		t.Fatalf("Other was not attached to its question: %+v", form.Questions)
	}
	if form.Questions[0].Other == nil || form.Questions[1].Other == nil {
		t.Fatal("missing Other companions")
	}
	view := ansi.Strip(form.View(64, 20))
	for _, expected := range []string{"Which approach", "Small changes", "Larger changes", "Other…", "Preview", "Keep the public API stable."} {
		if !strings.Contains(view, expected) {
			t.Fatalf("question view missing %q:\n%s", expected, view)
		}
	}
	if strings.Count(view, "Which approach") != 1 {
		t.Fatalf("form message or prompt rendered twice:\n%s", view)
	}

	// The cursor is not the answer: arrowing to Fast does not choose it.
	press(form, tea.KeyDown)
	if len(form.Questions[0].Field.Selected) != 0 {
		t.Fatal("moving selected an answer")
	}
	if action := press(form, tea.KeyEnter); action != ActionNone {
		t.Fatal("multi-question form submitted before all questions")
	}
	if form.Cursor != 1 || !reflect.DeepEqual(form.Questions[0].Field.Selected, []string{"fast"}) {
		t.Fatalf("Enter did not choose and advance: cursor=%d answers=%v", form.Cursor, form.Questions[0].Field.Selected)
	}

	// Space toggles multi-select answers; Other adds text alongside them.
	press(form, tea.KeySpace)
	press(form, tea.KeyDown)
	press(form, tea.KeySpace)
	press(form, tea.KeyDown)
	if !form.Questions[1].onOther() {
		t.Fatal("down did not reach Other")
	}
	text(form, "lint")
	press(form, tea.KeyEnter)
	if !form.Review {
		t.Fatal("last question did not open the review")
	}
	review := ansi.Strip(form.View(64, 20))
	for _, expected := range []string{"Fast", "Unit tests", "Race tests", "Other: lint", "Submit answers"} {
		if !strings.Contains(review, expected) {
			t.Fatalf("review missing %q:\n%s", expected, review)
		}
	}
	if strings.Contains(review, `["unit,tests"`) {
		t.Fatalf("review leaked raw JSON:\n%s", review)
	}
	if action := press(form, tea.KeyEnter); action != ActionSubmit {
		t.Fatal("Submit row did not submit")
	}
	if form.Content["question_0"] != "fast" || !reflect.DeepEqual(form.Content["question_1"], []string{"unit,tests", "race"}) || form.Content["question_1_custom"] != "lint" {
		t.Fatalf("wrong response: %#v", form.Content)
	}

	// Claude's single-select Other replaces the choice rather than adding a
	// note, and selecting an option clears the Other text.
	form, err = NewForm(json.RawMessage(`{"type":"object","properties":{"question_0":{"type":"string","title":"Approach","oneOf":[{"const":"safe","title":"Safe"},{"const":"fast","title":"Fast"}]},"question_0_custom":{"type":"string","title":"Other"},"question_1":{"type":"array","title":"Checks","items":{"anyOf":[{"const":"unit","title":"Unit"}]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	press(form, tea.KeyDown)
	press(form, tea.KeyEnter)
	pressMsg(form, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	press(form, tea.KeyDown)
	if !form.Questions[0].onOther() {
		t.Fatal("did not reach Other")
	}
	text(form, "custom")
	if len(form.Questions[0].Field.Selected) != 0 {
		t.Fatal("typing Other kept the selected option")
	}
	press(form, tea.KeyEnter)
	action, _ := form.Key(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if action != ActionSubmit {
		t.Fatal("Ctrl+Enter did not submit")
	}
	if _, ok := form.Content["question_0"]; ok {
		t.Fatalf("Other should replace the choice: %#v", form.Content)
	}
	if form.Content["question_0_custom"] != "custom" {
		t.Fatalf("missing custom answer: %#v", form.Content)
	}
}

func TestBooleanFieldsRenderAsChoices(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"enabled":{"type":"boolean","title":"Enabled"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fieldOptionValues(form.Questions[0].Field.Options), []string{"false", "true"}) {
		t.Fatalf("wrong boolean choices: %+v", form.Questions[0].Field.Options)
	}
	view := ansi.Strip(form.View(40, 12))
	for _, expected := range []string{"No", "Yes", "● No"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("boolean view missing %q:\n%s", expected, view)
		}
	}
	press(form, tea.KeyDown)
	if action := press(form, tea.KeyEnter); action != ActionSubmit {
		t.Fatal("boolean question did not submit")
	}
	if form.Content["enabled"] != true {
		t.Fatalf("wrong boolean answer: %#v", form.Content)
	}
}

func TestOtherInputKeepsCursorVisible(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","title":"Pick","enum":["a","b"]},"q_custom":{"type":"string","title":"Other"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	press(form, tea.KeyDown)
	press(form, tea.KeyDown)
	text(form, "abcdefghijklmnopqrstuvwxyz0123456789")
	view := ansi.Strip(form.View(32, 12))
	if !strings.Contains(view, "0123456789") {
		t.Fatalf("typed tail was clipped:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 32 {
			t.Fatalf("width exceeded:\n%s", view)
		}
	}
}

func TestSingleLineLabelsDoNotBreakRows(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"one":{"type":"string","title":"First\nChoice\tNow","oneOf":[{"const":"safe","title":"Safe\nPath"},{"const":"fast","title":"Fast\tTrack"}]},"two":{"type":"string","title":"Second\nChoice","enum":["x","y"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	view := ansi.Strip(form.View(64, 20))
	if !strings.Contains(view, "First Choice Now") || !strings.Contains(view, "Safe Path") {
		t.Fatalf("labels were not collapsed:\n%s", view)
	}
	if strings.ContainsAny(view, "\t") {
		t.Fatalf("tab leaked into the view:\n%q", view)
	}
	if lines := strings.Split(view, "\n"); len(lines) != 20 {
		t.Fatalf("embedded label newline changed the row count: %d\n%s", len(lines), view)
	}
	press(form, tea.KeyEnter)
	press(form, tea.KeyEnter)
	if !form.Review {
		t.Fatal("review did not open")
	}
	view = ansi.Strip(form.View(64, 20))
	if !strings.Contains(view, "First Choice Now: Safe Path") {
		t.Fatalf("review labels were not collapsed:\n%s", view)
	}
	if lines := strings.Split(view, "\n"); len(lines) != 20 {
		t.Fatalf("review row count changed: %d\n%s", len(lines), view)
	}
}

func TestQuestionOrderAndSingleQuestionSubmit(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"question_10":{"type":"string","title":"Ten","enum":["a"]},"question_2":{"type":"string","title":"Two","enum":["b"]},"question_2_custom":{"type":"string","title":"Other"},"question_1":{"type":"string","title":"One","enum":["c"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, question := range form.Questions {
		labels = append(labels, question.Field.Label)
	}
	if !reflect.DeepEqual(labels, []string{"One", "Two", "Ten"}) {
		t.Fatalf("wrong question order: %v", labels)
	}
	if form.Questions[1].Other == nil {
		t.Fatal("companion did not follow natural order")
	}

	form, err = NewForm(json.RawMessage(`{"type":"object","properties":{"choice":{"type":"string","title":"Pick","enum":["a","b"],"enumNames":["Alpha","Beta"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	press(form, tea.KeyDown)
	if action := press(form, tea.KeyEnter); action != ActionSubmit {
		t.Fatal("single question did not submit on Enter")
	}
	if form.Content["choice"] != "b" {
		t.Fatalf("wrong choice: %#v", form.Content)
	}
}

func TestRequiredOtherCompanionValidates(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","title":"Pick","enum":["a","b"]},"q_custom":{"type":"string","title":"Other"}},"required":["q_custom"]}`))
	if err != nil {
		t.Fatal(err)
	}
	press(form, tea.KeyEnter)
	if form.Error == "" || !strings.Contains(form.Error, "Other is required") {
		t.Fatalf("missing required Other error: %q", form.Error)
	}
	if !form.Questions[0].onOther() || !form.Questions[0].Other.Input.Focused() {
		t.Fatal("required Other did not receive focus")
	}
	text(form, "custom")
	if action, _ := form.Key(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}); action != ActionSubmit {
		t.Fatal("Ctrl+Enter did not submit the typed Other answer")
	}
	if form.Content["q_custom"] != "custom" {
		t.Fatalf("wrong answer: %#v", form.Content)
	}
}

func TestReviewEditsAnAnswerAndEscSkips(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"one":{"type":"string","title":"One","enum":["a","b"]},"two":{"type":"string","title":"Two","enum":["x","y"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	press(form, tea.KeyEnter) // choose "a" and advance
	press(form, tea.KeyEnter) // choose "x" and open review
	if !form.Review || form.ReviewCursor != 2 {
		t.Fatal("review did not open on the Submit row")
	}
	press(form, tea.KeyUp)
	if form.ReviewCursor != 1 {
		t.Fatal("review cursor did not move")
	}
	press(form, tea.KeyEnter)
	if form.Review || form.Cursor != 1 {
		t.Fatal("Enter did not reopen the answer for editing")
	}
	press(form, tea.KeyDown)
	press(form, tea.KeySpace)
	press(form, tea.KeyEnter)
	if !form.Review || form.ReviewCursor != 1 {
		t.Fatalf("edited answer did not return to its review row: review=%v cursor=%d", form.Review, form.ReviewCursor)
	}
	if view := ansi.Strip(form.View(48, 16)); !strings.Contains(view, "Two: y") {
		t.Fatalf("review did not show the edited label:\n%s", view)
	}
	if action := press(form, tea.KeyEscape); action != ActionSkip {
		t.Fatal("Esc in review did not skip")
	}
	action, _ := form.Key(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if action != ActionSubmit || form.Content["two"] != "y" {
		t.Fatalf("Ctrl+Enter did not send the edit: %#v", form.Content)
	}
}

func TestQuestionBoundsAndScrolling(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"choice":{"type":"string","enum":["a","b"],"enumNames":["Alpha","Beta"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	form.Message = strings.Repeat("Long question line\n", 50)
	for _, size := range [][2]int{{1, 1}, {12, 4}, {40, 12}} {
		view := form.View(size[0], size[1])
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatalf("height exceeded at %v:\n%s", size, ansi.Strip(view))
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("width exceeded at %v:\n%s", size, ansi.Strip(view))
			}
		}
	}
	form.View(40, 12)
	for range 30 {
		form.Key(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	if !strings.Contains(ansi.Strip(form.View(40, 12)), "Long question") {
		t.Fatal("question is not scrollable")
	}
	for range 30 {
		form.Key(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !strings.Contains(ansi.Strip(form.View(40, 12)), "Alpha") {
		t.Fatal("options not reachable")
	}
	if len(form.Questions) != 1 || len(form.Questions[0].Field.Selected) != 0 {
		t.Fatal("scrolling answered question")
	}
}

func TestFormRejectsUnsupportedNestedTypes(t *testing.T) {
	if _, err := NewForm(json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object"}}}`)); err == nil {
		t.Fatal("nested form accepted")
	}
}

func TestListFilteringAndEmptySelection(t *testing.T) {
	list := NewList("Test", []Item{{Label: "Model", Detail: "Reasoning"}, {Label: "Session"}}, true)
	list.Search.SetValue("reason")
	if item, ok := list.Selected(); !ok || item.Label != "Model" {
		t.Fatal("detail filtering failed")
	}
	list.Search.SetValue("nothing")
	if _, ok := list.Selected(); ok {
		t.Fatal("empty selection succeeded")
	}
}
