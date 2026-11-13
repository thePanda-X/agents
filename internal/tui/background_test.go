package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// unpaintedCells counts cells that fall back to the terminal default
// background. Windows consoles commonly ignore the OSC 11 background request,
// so every cell must carry an explicit background.
func unpaintedCells(content string, width, height int) int {
	buf := uv.NewScreenBuffer(width, height)
	uv.NewStyledString(content).Draw(buf, buf.Bounds())
	missing := 0
	for y := range height {
		for x := range width {
			if cell := buf.CellAt(x, y); cell == nil || cell.Style.Bg == nil {
				missing++
			}
		}
	}
	return missing
}

func TestViewPaintsEveryCellBackground(t *testing.T) {
	cases := []struct {
		name          string
		appearance    string
		width, height int
		overlay       bool
	}{
		{name: "default", width: 80, height: 30},
		{name: "high contrast", appearance: "high-contrast", width: 80, height: 30},
		{name: "overlay", width: 80, height: 30, overlay: true},
		{name: "too small", width: 24, height: 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := seedScrollableSession(t)
			m.opts.Settings.Appearance = tc.appearance
			m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			if tc.overlay {
				m.openInfo("Info", "Body text")
			}
			m.refresh()
			if missing := unpaintedCells(m.View().Content, tc.width, tc.height); missing > 0 {
				t.Fatalf("%d cells use the terminal default background", missing)
			}
		})
	}
}

func TestTerminalAppearanceKeepsDefaultBackground(t *testing.T) {
	m := seedScrollableSession(t)
	m.opts.Settings.Appearance = "terminal"
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.refresh()
	if missing := unpaintedCells(m.View().Content, 80, 30); missing == 0 {
		t.Fatal("terminal appearance must leave the background to the terminal")
	}
}

func TestTerminalDefaultColorsAreOptional(t *testing.T) {
	previous := setTerminalDefaultColors
	t.Cleanup(func() { setTerminalDefaultColors = previous })
	for _, size := range []struct{ width, height int }{{80, 30}, {24, 8}} {
		for _, enabled := range []bool{true, false} {
			setTerminalDefaultColors = enabled
			m := seedScrollableSession(t)
			m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			m.refresh()
			v := m.View()
			if got := v.BackgroundColor != nil; got != enabled {
				t.Fatalf("%dx%d enabled=%v: background color set = %v", size.width, size.height, enabled, got)
			}
			// Without OSC 11 every cell must still carry the app background.
			if missing := unpaintedCells(v.Content, size.width, size.height); missing > 0 {
				t.Fatalf("%dx%d enabled=%v: %d cells unpainted", size.width, size.height, enabled, missing)
			}
		}
	}
}
