// Package theme contains semantic styles. UI components should use roles rather
// than inventing colors locally.
package theme

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Remap semantic UI colors without process-global theme mutation. This also
// covers nested Markdown and overlay styles rendered by reusable components.
func HighContrast(content string) string {
	colors := strings.NewReplacer(
		"38;2;232;230;234", "38;2;255;255;255",
		"38;2;170;166;176", "38;2;219;215;227",
		"38;2;72;68;78", "38;2;166;161;179",
		"38;2;242;184;207", "38;2;255;209;224",
		"48;2;51;48;57", "48;2;69;64;75",
	)
	return sgr.ReplaceAllStringFunc(content, colors.Replace)
}

// Keep bold, underline, reverse-video focus/cursor cues, and resets while letting
// the terminal supply its own foreground/background colors (including light themes).
func TerminalDefault(content string) string {
	return sgr.ReplaceAllStringFunc(content, func(sequence string) string {
		parameters := strings.Split(sequence[2:len(sequence)-1], ";")
		var kept []string
		for index := 0; index < len(parameters); index++ {
			code, _ := strconv.Atoi(parameters[index])
			switch {
			case code == 38 || code == 48 || code == 58:
				if index+1 < len(parameters) {
					mode := parameters[index+1]
					index++
					if mode == "2" {
						index += 3
					} else if mode == "5" {
						index++
					}
				}
			case code >= 30 && code <= 49 || code >= 90 && code <= 107 || code == 59:
			default:
				kept = append(kept, parameters[index])
			}
		}
		if len(kept) == 0 {
			return ""
		}
		return "\x1b[" + strings.Join(kept, ";") + "m"
	})
}

var (
	Background = lipgloss.Color("#1B1B1F")
	Surface    = lipgloss.Color("#27272D")
	Selection  = lipgloss.Color("#333039")
	Pink       = lipgloss.Color("#F2B8CF")
	Text       = lipgloss.Color("#E8E6EA")
	Muted      = lipgloss.Color("#AAA6B0")
	Border     = lipgloss.Color("#48444E")
	Green      = lipgloss.Color("#A8C9B3")
	Amber      = lipgloss.Color("#E9C791")
	Red        = lipgloss.Color("#EBA0AC")
)

var (
	Body    = lipgloss.NewStyle().Foreground(Text)
	Quiet   = lipgloss.NewStyle().Foreground(Muted)
	Accent  = lipgloss.NewStyle().Foreground(Pink)
	Heading = Accent.Bold(true)
	Error   = lipgloss.NewStyle().Foreground(Red)
	Success = lipgloss.NewStyle().Foreground(Green)
	Panel   = Body.Background(Surface).Border(lipgloss.RoundedBorder()).BorderForeground(Border).Padding(1, 2)
)

// Clean prevents untrusted agent text from emitting terminal control sequences,
// including OSC clipboard operations, window-title changes, and cursor moves.
func Clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(text))
}

func Line(left, right string, width int) string {
	width = max(1, width)
	if right == "" {
		return ansi.Truncate(left, width, "…")
	}
	if lipgloss.Width(right)+3 >= width {
		return ansi.Truncate(right, width, "…")
	}
	left = ansi.Truncate(left, width-lipgloss.Width(right)-2, "…")
	return left + strings.Repeat(" ", max(1, width-lipgloss.Width(left)-lipgloss.Width(right))) + right
}
