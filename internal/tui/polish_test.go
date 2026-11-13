package tui

import (
	"strings"
	"testing"
	"time"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/tui/overlays"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterDescribesOpenDialogInsteadOfComposer(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.openPalette()
	footer := ansi.Strip(m.footer(96))
	if !strings.Contains(footer, "PICKER") || !strings.Contains(footer, "Esc:close") || strings.Contains(footer, "Enter:send") || strings.Contains(footer, "newline") {
		t.Fatalf("picker footer shows composer hints: %q", footer)
	}
	m.closeOverlay()
	m.openHelp()
	if footer := ansi.Strip(m.footer(96)); !strings.Contains(footer, "VIEW") || !strings.Contains(footer, "Esc:close") {
		t.Fatalf("info footer: %q", footer)
	}
	m.closeOverlay()
	if footer := ansi.Strip(m.footer(96)); !strings.Contains(footer, "COMPOSE") || !strings.Contains(footer, "Enter:send") {
		t.Fatalf("composer hints did not return: %q", footer)
	}
}

func TestCompactLayoutKeepsFooterHints(t *testing.T) {
	m := testModel(t)
	m.opts.Settings.Compact = true
	if footer := ansi.Strip(m.footer(96)); !strings.Contains(footer, "Ctrl+J:newline") || !strings.Contains(footer, "Tab:browse") {
		t.Fatalf("compact density hid footer hints: %q", footer)
	}
}

func TestPaletteOmitsItselfGroupsActionsAndTeachesSlashCommands(t *testing.T) {
	m := testModel(t)
	m.perform("palette")
	m.closeOverlay()
	m.perform("compact")
	m.perform("palette")
	if !m.list.GroupHeaders {
		t.Fatal("palette groups are not shown as headers")
	}
	var recent []string
	for _, item := range m.list.Items {
		if item.ID == "palette" {
			t.Fatal("palette lists itself")
		}
		if item.Group == "Recent" {
			recent = append(recent, item.ID)
		}
		if item.ID == "restore_draft" && item.Group != "Recent" && item.Detail != "/restore" {
			t.Fatalf("unbound action should show its slash command, got %q", item.Detail)
		}
		if item.ID == "new_session" && item.Group != "Recent" && (!strings.Contains(item.Preview, "Command: /new") || !strings.Contains(item.Preview, "Shortcut: Ctrl+N")) {
			t.Fatalf("preview does not explain the action: %q", item.Preview)
		}
		if item.Detail == "—" {
			t.Fatalf("placeholder dash shown for %s", item.ID)
		}
	}
	if len(recent) != 1 || recent[0] != "compact" {
		t.Fatalf("recent group should hold the last action only, got %v", recent)
	}
}

func TestNarrowPickerHintsKeepVerbs(t *testing.T) {
	l := overlays.NewList("Pick", []overlays.Item{{ID: "a", Label: "a"}}, true)
	if view := ansi.Strip(l.View(50, 12)); !strings.Contains(view, "Enter choose") {
		t.Fatalf("narrow hint lost its verb: %s", view)
	}
}

func TestContextMeterIsLabeledAndWarnsNearTheLimit(t *testing.T) {
	for n, want := range map[uint64]string{200000: "200k", 48200: "48.2k", 1000000: "1M", 1250000: "1.2M", 950: "950"} {
		if got := tokens(n); got != want {
			t.Fatalf("tokens(%d) = %q, want %q", n, got, want)
		}
	}
	if got := ansi.Strip(contextUsage(48200, 200000)); got != "ctx 48.2k/200k 24%" {
		t.Fatalf("context label = %q", got)
	}
	m := testModel(t)
	m.state.Active().Usage = &harness.Usage{Used: 170000, Size: 200000}
	header := m.header(100)
	if !strings.Contains(ansi.Strip(header), "ctx 170k/200k 85%") || !strings.Contains(header, "38;2;233;199;145") {
		t.Fatalf("near-full context is not highlighted: %q", header)
	}
}

func TestPermissiveModeIsHighlightedInComposerBorder(t *testing.T) {
	mode := func(value, name string) []harness.ConfigOption {
		return []harness.ConfigOption{{Category: "mode", Value: value, Choices: []harness.Choice{{Value: value, Name: name}}}}
	}
	m := testModel(t)
	m.opts.Descriptor.PermissiveModes = []string{"auto"}
	for _, tc := range []struct {
		value, name string
		warn        bool
	}{{"auto", "Auto", true}, {"bypassPermissions", "Bypass Permissions", true}, {"plan", "Plan", false}} {
		m.state.Active().Options = mode(tc.value, tc.name)
		meta := m.composerMeta()
		if got := strings.Contains(ansi.Strip(meta), "⚠ "+tc.name); got != tc.warn {
			t.Fatalf("%s: warning=%v, label %q", tc.value, got, ansi.Strip(meta))
		}
	}
}

func TestDialogDimsTheConversationBehindIt(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state.Active().Entries = []*app.Entry{{ID: "t", Role: "tool", Tool: &app.ToolState{Title: "Run", Status: "completed"}}}
	m.refresh()
	dim := m.dimmed(m.header(96) + "\n" + m.footer(96))
	if strings.Contains(dim, "38;2;242;184;207") || !strings.Contains(dim, "38;2;72;68;78") {
		t.Fatalf("dimmed layer keeps accent colors: %q", dim)
	}
	m.opts.Settings.Appearance = "terminal"
	if dim := m.dimmed("text"); !strings.Contains(dim, "\x1b[2m") {
		t.Fatalf("terminal appearance should use faint text: %q", dim)
	}
}

func TestClockSettingFormatsSentMessageTimes(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state.Active().Entries = []*app.Entry{{ID: "u", Role: "user", Text: "hello", At: time.Date(2026, 11, 13, 21, 5, 0, 0, time.Local)}}
	m.dirty = true
	m.refresh()
	if !strings.Contains(ansi.Strip(m.viewport.View()), "9:05 PM") {
		t.Fatal("default clock is not 12-hour")
	}
	m.openSettings()
	for _, item := range m.list.Items {
		if item.ID == "clock24" {
			m.toggleSetting(item)
		}
	}
	m.closeOverlay()
	m.refresh()
	if !m.opts.Settings.Clock24 || !strings.Contains(ansi.Strip(m.viewport.View()), "21:05") {
		t.Fatal("24-hour setting did not re-render message times")
	}
}
