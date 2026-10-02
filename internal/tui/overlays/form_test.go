package overlays

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFormDefaultsTypedAnswersAndValidation(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","default":"Alex","minLength":2},"count":{"type":"integer","default":2,"minimum":1},"enabled":{"type":"boolean","default":true},"strategy":{"type":"string","enum":["safe","fast"],"default":"safe"}},"required":["name","count","enabled","strategy"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !form.Prepare() {
		t.Fatal(form.Error)
	}
	if !form.Review || form.Content["enabled"] != true || form.Content["count"] != int64(2) || form.Content["name"] != "Alex" {
		t.Fatalf("wrong typed content: %+v", form.Content)
	}
	form.Review = false
	for i := range form.Fields {
		if form.Fields[i].Name == "count" {
			form.Fields[i].Input.SetValue("0")
		}
	}
	if form.Prepare() || form.Error == "" {
		t.Fatal("invalid minimum accepted")
	}
}

func TestClaudeQuestionSelectionsAndOther(t *testing.T) {
	form, err := NewForm(json.RawMessage(`{"type":"object","properties":{"q0":{"type":"string","title":"Approach","description":"Which approach should we use?","oneOf":[{"const":"safe","title":"Safe","description":"Small changes"},{"const":"fast","title":"Fast","description":"Larger changes"}]},"q0_other":{"type":"string","title":"Other"},"q1":{"type":"array","title":"Checks","items":{"anyOf":[{"const":"unit,tests","title":"Unit tests"},{"const":"race","title":"Race tests"}]},"minItems":1,"uniqueItems":true}},"required":["q1"]}`))
	if err != nil {
		t.Fatal(err)
	}
	press := func(code rune) { form.Update(tea.KeyPressMsg{Code: code}) }
	view := ansi.Strip(form.View(60, 16))
	if !strings.Contains(view, "Which approach") || !strings.Contains(view, "Small changes") {
		t.Fatal(view)
	}
	press(tea.KeyDown)
	if form.Fields[0].Input.Value() != "" {
		t.Fatal("moving selected an answer")
	}
	press(tea.KeyEnter)
	press(tea.KeyTab)
	form.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if form.Fields[1].Input.Value() != "k" {
		t.Fatal("text input intercepted navigation letter")
	}
	press(tea.KeyTab)
	if form.Prepare() {
		t.Fatal("required array accepted without answer")
	}
	press(tea.KeySpace)
	press(tea.KeyDown)
	press(tea.KeySpace)
	if !form.Prepare() {
		t.Fatal(form.Error)
	}
	if form.Content["q0"] != "fast" || form.Content["q0_other"] != "k" || !reflect.DeepEqual(form.Content["q1"], []string{"unit,tests", "race"}) {
		t.Fatalf("wrong response: %#v", form.Content)
	}
	form.Edit()
	press(tea.KeySpace)
	if !form.Prepare() || !reflect.DeepEqual(form.Content["q1"], []string{"unit,tests"}) {
		t.Fatalf("toggle failed: %#v", form.Content)
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
			t.Fatal("height exceeded")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("width exceeded")
			}
		}
	}
	form.View(40, 12)
	for range 30 {
		form.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	if !strings.Contains(ansi.Strip(form.View(40, 12)), "Long question") {
		t.Fatal("question is not scrollable")
	}
	for range 30 {
		form.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !strings.Contains(ansi.Strip(form.View(40, 12)), "Alpha") {
		t.Fatal("options not reachable")
	}
	if form.Fields[0].Input.Value() != "" {
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
