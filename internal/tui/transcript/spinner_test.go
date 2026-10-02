package transcript

import (
	"strings"
	"testing"

	"agents/internal/app"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPendingAndRunningToolsUseSpinnerEvenWhenExpanded(t *testing.T) {
	for _, status := range []string{"pending", "in_progress", "completed", "failed", "cancelled"} {
		for _, expanded := range []bool{false, true} {
			for _, compact := range []bool{false, true} {
				c := &app.Conversation{Entries: []*app.Entry{{ID: "tool", Tool: &app.ToolState{Title: "Patch", Status: status}}, {Role: "assistant", Text: "AFTER TOOL"}}}
				opts := Options{Width: 60, Compact: compact, Expanded: map[string]bool{"tool": expanded}}
				first, positions := New().Render(c, opts)
				opts.SpinnerFrame = 1
				second, nextPositions := New().Render(c, opts)
				if lipgloss.Height(first) != lipgloss.Height(second) || positions[1] != nextPositions[1] {
					t.Fatal("spinner changed transcript geometry")
				}
				icon := "▸"
				if expanded {
					icon = "▾"
				}
				if status == "pending" || status == "in_progress" {
					icon = toolSpinnerFrames[1]
					if first == second {
						t.Fatal("active spinner did not change frames")
					}
				} else if first != second {
					t.Fatal("finished tool kept animating")
				}
				if !strings.HasPrefix(strings.TrimSpace(ansi.Strip(second)), icon+" Patch") {
					t.Fatalf("wrong tool icon for %s (expanded=%v)", status, expanded)
				}
			}
		}
	}
}
