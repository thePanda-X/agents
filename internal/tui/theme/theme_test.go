package theme

import (
	"strings"
	"testing"
)

func TestCleanRemovesTerminalInjection(t *testing.T) {
	text := Clean("safe\x1b]52;c;c2VjcmV0\a\x1b[2J\x1b[31mred\x1b[0m\r\nnext")
	if strings.ContainsAny(text, "\x1b\a\r") || !strings.Contains(text, "safe") || !strings.Contains(text, "next") {
		t.Fatalf("unsafe cleaned text: %q", text)
	}
}

func TestHighContrastOnlyRemapsANSIStyles(t *testing.T) {
	text := "plain 38;2;72;68;78 text\x1b[1;38;2;72;68;78mBorder\x1b[0m"
	result := HighContrast(text)
	if !strings.Contains(result, "plain 38;2;72;68;78 text") || !strings.Contains(result, "\x1b[1;38;2;166;161;179m") {
		t.Fatal("high contrast changed plain content or failed to remap compound SGR styles")
	}
}

func TestTerminalDefaultPreservesReverseCursorAndBold(t *testing.T) {
	result := TerminalDefault("\x1b[1;38;2;242;184;207;48;5;255mBold\x1b[0m \x1b[7;31mCursor\x1b[0m")
	if !strings.Contains(result, "\x1b[1mBold") || !strings.Contains(result, "\x1b[7mCursor") || strings.Contains(result, "38;") || strings.Contains(result, "48;") {
		t.Fatalf("terminal appearance lost emphasis/cursor or retained colors: %q", result)
	}
}

func TestTextSelectionUsesPlumWithoutReverseOrBold(t *testing.T) {
	result := TextSelection.Render("selected")
	if !strings.Contains(result, "48;2;73;51;64") || !strings.Contains(result, "38;2;232;230;234") {
		t.Fatalf("selection lost its plum background or light text: %q", result)
	}
	if TextSelection.GetReverse() || TextSelection.GetBold() {
		t.Fatal("text selection should not use reverse video or bold")
	}
}

func TestHighContrastTextSelection(t *testing.T) {
	result := HighContrast(TextSelection.Render("selected"))
	if !strings.Contains(result, "48;2;96;62;81") || !strings.Contains(result, "38;2;255;255;255") {
		t.Fatalf("high contrast did not remap selection background and text: %q", result)
	}
}

func TestTerminalTextSelectionRemainsVisibleWithoutApplicationColors(t *testing.T) {
	result := TerminalDefault(TerminalTextSelection.Render("selected"))
	if !strings.Contains(result, "\x1b[7mselected") || strings.Contains(result, "38;") || strings.Contains(result, "48;") {
		t.Fatalf("terminal selection lost reverse video or retained application colors: %q", result)
	}
}

func TestFillBackgroundReappliesAppBackgroundAfterResets(t *testing.T) {
	const fill = "48;2;27;27;31"
	for input, want := range map[string]string{
		"\x1b[m":                     "\x1b[;" + fill + "m",
		"\x1b[0m":                    "\x1b[0;" + fill + "m",
		"\x1b[1;49m":                 "\x1b[1;49;" + fill + "m",
		"\x1b[0;48;2;1;2;3m":         "\x1b[0;48;2;1;2;3m",
		"\x1b[0;44m":                 "\x1b[0;44m",
		"\x1b[38;2;0;0;0m":           "\x1b[38;2;0;0;0m",
		"\x1b[38;5;0m":               "\x1b[38;5;0m",
		"\x1b[48;2;1;2;3;0m":         "\x1b[48;2;1;2;3;0;" + fill + "m",
		"plain text without escapes": "plain text without escapes",
	} {
		if got := FillBackground(input); got != want {
			t.Errorf("FillBackground(%q) = %q, want %q", input, got, want)
		}
	}
}
