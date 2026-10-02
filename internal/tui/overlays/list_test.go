package overlays

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFuzzyRankingAndUnicode(t *testing.T) {
	l := NewList("Files", []Item{{ID: "1", Label: "internal/tui/model.go"}, {ID: "2", Label: "model"}, {ID: "3", Label: "界面.go"}}, true)
	l.Search.SetValue("mdl")
	if len(l.Filtered()) != 2 {
		t.Fatal("subsequence search failed")
	}
	l.Search.SetValue("model")
	if l.Filtered()[0].ID != "2" {
		t.Fatal("prefix match did not rank first")
	}
	l.Search.SetValue("界")
	if selected, ok := l.Selected(); !ok || selected.ID != "3" {
		t.Fatal("unicode match failed")
	}
	if view := l.View(40, 12); !strings.Contains(ansi.Strip(view), "界面.go") || !strings.Contains(view, "\x1b[") {
		t.Fatal("matching text not highlighted")
	}
}

func TestPickerNavigationMultiSelectAndPreviewFits(t *testing.T) {
	l := NewList("Files", []Item{{ID: "a", Label: "a.go", Preview: strings.Repeat("long preview 界\n", 50)}, {ID: "b", Label: "b.go"}}, true)
	l.MultiSelect = true
	l.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if len(l.MarkedItems()) != 1 || l.Cursor != 1 {
		t.Fatal("Tab did not mark and advance")
	}
	l.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if l.Cursor != 0 {
		t.Fatal("Ctrl+P did not navigate picker")
	}
	for _, width := range []int{40, 100} {
		view := l.View(width, 15)
		if w, h := lipgloss.Size(view); w > width || h > 15 {
			t.Fatalf("picker overflow: %dx%d", w, h)
		}
		if width == 100 && !strings.Contains(ansi.Strip(view), "Preview") {
			t.Fatal("wide picker missing preview")
		}
	}
}

func TestLongMarkedSelectionFitsStrictRowBudget(t *testing.T) {
	items := []Item{{ID: "a", Label: strings.Repeat("long-file-name-界", 8)}}
	for _, id := range []string{"b", "c", "d", "e", "f", "g"} {
		items = append(items, Item{ID: id, Label: id + ".go"})
	}
	l := NewList("Files", items, true)
	l.MultiSelect, l.Marked["a"] = true, true
	for _, size := range [][2]int{{26, 8}, {40, 10}, {100, 15}} {
		view := l.View(size[0], size[1])
		if w, h := lipgloss.Size(view); w > size[0] || h > size[1] {
			t.Fatalf("marked selection overflowed %v: %dx%d\n%s", size, w, h, ansi.Strip(view))
		}
		if !strings.Contains(ansi.Strip(view), "› [x]") || !strings.Contains(ansi.Strip(view), "Esc") {
			t.Fatal("selection, mark, or dismissal hint disappeared")
		}
	}
}
