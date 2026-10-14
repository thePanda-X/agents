package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/storage"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func ctrl(code rune) tea.KeyPressMsg   { return tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl} }
func letter(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: string(code)} }

func TestComposerGrowsForSoftWrapAndShrinksAfterDeletion(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.Update(tea.WindowSizeMsg{Width: 44, Height: 24})
	text := strings.Repeat("界🙂 words ", 16) + "END"
	m.Update(tea.PasteMsg{Content: text})
	if m.input.LineCount() != 1 || m.input.Height() <= 1 || m.input.Height() > 4 {
		t.Fatalf("soft-wrap sizing: logical=%d visual=%d", m.input.LineCount(), m.input.Height())
	}
	if m.input.Value() != text || !strings.Contains(ansi.Strip(m.input.View()), "END") {
		t.Fatal("overflow lost text or hid the cursor at the end")
	}
	m.input.SetValue("short")
	m.resize()
	if m.input.Height() != 1 {
		t.Fatal("composer did not shrink")
	}
}

func TestCompactShortcutPreservesDraftFocusAndReadingAnchor(t *testing.T) {
	m := testModel(t)
	if m.actionFor("ctrl+x") != "compact" || m.actionFor("alt+c") != "" {
		t.Fatal("compact default binding was not changed to Ctrl+X")
	}
	m.state.Active().Entries = []*app.Entry{{Role: "user", Text: "Prompt"}, {Role: "assistant", Text: strings.Repeat("Response line\n\n", 80)}}
	m.input.SetValue("keep my draft")
	m.input.SetCursorColumn(4)
	m.refresh()
	m.setTranscriptFocus(true)
	m.selectEntry(1)
	m.viewport.SetYOffset(m.positions[1] + 5)
	m.browseLine = m.positions[1] + 6
	for _, want := range []bool{true, false} {
		m.handleKey(ctrl('x'))
		if m.opts.Settings.Compact != want {
			t.Fatal("Ctrl+X did not toggle density")
		}
		if !m.focusedTranscript || m.selected != 1 || m.input.Value() != "keep my draft" || m.input.Column() != 4 {
			t.Fatal("toggle changed focus, selection, or draft")
		}
		if m.viewport.YOffset()-m.positions[1] != 5 || m.browseLine-m.positions[1] != 6 {
			t.Fatal("toggle moved reading anchor")
		}
	}
}

func TestCompactShortcutIsRemappableDiscoverableAndPersisted(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.opts.Store = &storage.Store{Directory: t.TempDir()}
	m.opts.Settings.Keybindings = map[string]string{"compact": "ctrl+l"}
	m.handleKey(ctrl('x'))
	if m.opts.Settings.Compact {
		t.Fatal("default toggle survived explicit remap")
	}
	cmd := m.handleKey(ctrl('l'))
	if cmd == nil || !m.opts.Settings.Compact {
		t.Fatal("remapped toggle did not save preference")
	}
	if result := cmd().(saveMsg); result.err != nil {
		t.Fatal(result.err)
	}
	settings, _, err := m.opts.Store.Load()
	if err != nil || !settings.Compact {
		t.Fatal("density preference not persisted")
	}
	m.openPalette()
	found := false
	for _, item := range m.list.Items {
		if item.ID == "compact" {
			found = item.Detail == "Ctrl+L"
		}
	}
	if !found {
		t.Fatal("palette did not expose remapped density shortcut")
	}
	m.openHelp()
	if !strings.Contains(m.infoContent, "Ctrl+L") || !strings.Contains(m.infoContent, "Toggle compact layout") {
		t.Fatal("density shortcut missing from help")
	}
}

func TestComposerAllowsMoreNewlinesThanVisibleRows(t *testing.T) {
	for _, newline := range []tea.KeyPressMsg{ctrl('j'), {Code: tea.KeyEnter, Mod: tea.ModAlt}, {Code: tea.KeyEnter, Mod: tea.ModShift}} {
		m := testModel(t)
		m.input.Focus()
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		for range 20 {
			m.Update(newline)
			m.Update(letter('x'))
		}
		if m.input.LineCount() != 21 {
			t.Fatalf("%s truncated newlines at %d", newline.String(), m.input.LineCount())
		}
		if m.input.Height() != 4 || m.input.ScrollYOffset() == 0 {
			t.Fatal("long draft does not scroll within bounded composer")
		}
		if m.submitting {
			t.Fatal("newline shortcut sent prompt")
		}
	}
}

func TestComposerResizePreservesDraftCursorAndFitsLayouts(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := testModel(t)
		m.opts.Settings.Compact = compact
		m.input.Focus()
		text := strings.Repeat("draft content with unicode 界\n", 25)
		m.Update(tea.PasteMsg{Content: text})
		line, column := m.input.Line(), m.input.Column()
		for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 14}, {32, 12}, {120, 40}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.refresh()
			if m.input.Value() != text || m.input.Line() != line || m.input.Column() != column {
				t.Fatal("resize moved cursor or changed draft")
			}
			if m.input.Height() > 4 {
				t.Fatal("resizing exceeded the four-row composer limit")
			}
			if width, height := lipgloss.Size(m.View().Content); width > size[0] || height > size[1] {
				t.Fatalf("layout exceeds terminal %v", size)
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "COMPOSE") {
				t.Fatalf("statusline clipped at %v compact=%v input=%d viewport=%d:\n%s", size, compact, m.input.Height(), m.viewport.Height(), ansi.Strip(m.View().Content))
			}
		}
	}
}

func TestComposerGrowsToFourRowsThenScrolls(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := testModel(t)
		m.opts.Settings.Compact = compact
		m.input.Focus()
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		for rows := 1; rows <= 6; rows++ {
			if rows > 1 {
				m.Update(ctrl('j'))
			}
			m.Update(letter('x'))
			if m.input.Height() != min(rows, 4) || m.input.LineCount() != rows {
				t.Fatalf("composer rows=%d: height=%d logical=%d", rows, m.input.Height(), m.input.LineCount())
			}
			if m.input.Value() != strings.Repeat("x\n", rows-1)+"x" {
				t.Fatal("four-row limit truncated a longer draft")
			}
		}
		if m.input.ScrollYOffset() == 0 || !strings.Contains(ansi.Strip(m.input.View()), "x") {
			t.Fatal("longer draft did not scroll inside the four-row composer")
		}
		if width, height := lipgloss.Size(m.View().Content); width > 80 || height > 24 {
			t.Fatal("four-row composer overflowed the terminal")
		}
		m.input.SetValue("short")
		m.resize()
		if m.input.Height() != 1 {
			t.Fatal("composer did not shrink back to one row")
		}
	}
}

func TestComposerWrappedCursorStaysVisibleAcrossResize(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := testModel(t)
		m.opts.Settings.Compact = compact
		m.input.Focus()
		text := strings.Repeat("界🙂 wrapped words ", 30) + "END"
		m.Update(tea.PasteMsg{Content: text})
		line, column := m.input.Line(), m.input.Column()
		for _, focused := range []bool{true, false} {
			if !focused {
				m.input.Blur()
			}
			for _, size := range [][2]int{{120, 40}, {44, 24}, {32, 12}, {80, 24}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				if m.input.Height() > 4 || !strings.Contains(ansi.Strip(m.input.View()), "END") {
					t.Fatalf("wrapped cursor hidden after resize to %v: height=%d offset=%d", size, m.input.Height(), m.input.ScrollYOffset())
				}
				if m.input.Value() != text || m.input.Line() != line || m.input.Column() != column || m.input.Focused() != focused {
					t.Fatal("viewport refresh changed the draft, cursor or focus")
				}
			}
		}
	}
}

func TestResponseUsesTerminalWidthAfterResizeAndStreaming(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := testModel(t)
		m.opts.Settings.Compact = compact
		m.input.SetValue("keep my draft")
		m.Update(eventMsg(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{ID: "reply", Role: "assistant", Text: strings.Repeat("response 界 words ", 100)}}))
		for _, width := range []int{40, 160, 240, 60, 160} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
			m.Update(tickMsg{})
			for _, focused := range []bool{false, true} {
				m.setTranscriptFocus(focused)
				widest := 0
				for _, line := range strings.Split(ansi.Strip(m.viewport.GetContent()), "\n") {
					if got := ansi.StringWidth(line); got > m.viewport.Width() {
						t.Fatalf("response exceeds viewport after resize to %d columns", width)
					}
					widest = max(widest, ansi.StringWidth(strings.TrimRight(line, " ")))
				}
				if widest < width-16 {
					t.Fatalf("response does not fill terminal: content=%d terminal=%d compact=%v focused=%v", widest, width, compact, focused)
				}
				if w, h := lipgloss.Size(m.View().Content); w > width || h > 30 {
					t.Fatal("response layout exceeds terminal")
				}
			}
			m.Update(eventMsg(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{ID: "reply", Role: "assistant", Text: " STREAMED"}}))
			m.Update(tickMsg{})
			if !strings.Contains(ansi.Strip(m.viewport.GetContent()), "STREAMED") || m.input.Value() != "keep my draft" {
				t.Fatal("reflow lost streamed text or changed the draft")
			}
		}
	}
}

func TestCtrlCDismissesBeforeCancellingAndClearIsRecoverable(t *testing.T) {
	m := testModel(t)
	m.state.Active().Busy = true
	m.openPalette()
	if cmd := m.handleKey(ctrl('c')); cmd != nil || m.list != nil || m.state.Active().Cancelling {
		t.Fatal("closing picker stopped turn")
	}
	i := &harness.Interaction{ID: "permission", Kind: "permission", Reply: make(chan harness.Decision, 1)}
	m.interaction = i
	if cmd := m.handleKey(ctrl('c')); cmd != nil || m.state.Active().Cancelling {
		t.Fatal("closing interaction stopped turn")
	}
	if decision := <-i.Reply; decision.Action != "cancel" {
		t.Fatal("interaction not cancelled")
	}
	if cmd := m.handleKey(ctrl('c')); cmd == nil || !m.state.Active().Cancelling {
		t.Fatal("main-view Ctrl+C did not stop turn")
	}
	m.state.Active().Busy = false
	m.input.SetValue("recover me\nsecond line")
	m.handleKey(ctrl('c'))
	m.perform("restore_draft")
	if m.input.Value() != "recover me\nsecond line" {
		t.Fatal("cleared draft could not be restored")
	}
	m.input.Reset()
	if cmd := m.handleKey(ctrl('c')); cmd != nil {
		t.Fatal("empty Ctrl+C unexpectedly quit")
	}
}

func TestQuitRemapAndGeneratedHints(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.opts.Settings.Keybindings = map[string]string{"quit": "alt+q", "palette": "alt+p"}
	if cmd := m.handleKey(ctrl('d')); cmd != nil {
		t.Fatal("old quit shortcut remains active")
	}
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt}); cmd == nil {
		t.Fatal("remapped quit shortcut does not work")
	}
	m.openPalette()
	for _, item := range m.list.Items {
		if item.ID == "quit" && item.Detail != "Alt+Q" {
			t.Fatal("palette hints ignore remap")
		}
	}
	m.openHelp()
	if !strings.Contains(m.infoContent, "Alt+Q") || !strings.Contains(m.infoContent, "Alt+P") {
		t.Fatal("help ignores remap")
	}
	if !strings.Contains(ansi.Strip(m.footer(100)), "Alt+P") {
		t.Fatal("footer ignores remap")
	}
}

func TestVimFocusKeepsCursorSelectionAndScroll(t *testing.T) {
	m := testModel(t)
	m.opts.Settings.VimNavigation = true
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: strings.Repeat("response line\n\n", 100)}}
	m.input.SetValue("draft text")
	m.input.SetCursorColumn(3)
	m.refresh()
	m.viewport.SetYOffset(10)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.focusedTranscript || m.input.Focused() {
		t.Fatal("Esc did not enter browse")
	}
	m.handleKey(ctrl('d'))
	if m.viewport.YOffset() <= 10 {
		t.Fatal("Ctrl+D did not half-page scroll")
	}
	offset := m.viewport.YOffset()
	m.handleKey(letter('i'))
	m.refresh()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.refresh()
	if m.input.Column() != 3 || m.input.Value() != "draft text" || m.viewport.YOffset() != offset {
		t.Fatal("focus switch moved draft or reading position")
	}
	m.handleKey(letter('g'))
	m.handleKey(letter('g'))
	if m.viewport.YOffset() != 0 {
		t.Fatal("gg did not jump to top")
	}
	m.handleKey(letter('G'))
	if !m.viewport.AtBottom() {
		t.Fatal("G did not jump to latest")
	}
}

func TestJKScrollsWithinLongEntryAndSurvivesRefresh(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := testModel(t)
		m.opts.Settings.VimNavigation, m.opts.Settings.Compact = true, compact
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: strings.Repeat("History line\n\n", 80)}}
		m.refresh()
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.Update(letter('G'))
		bottom := m.viewport.YOffset()
		if bottom < 5 {
			t.Fatal("fixture has no scrollable history")
		}
		for step := 1; step <= 5; step++ {
			m.Update(letter('k'))
			m.Update(tickMsg{})
			if got := m.viewport.YOffset(); got != bottom-step {
				t.Fatalf("k did not scroll a visual row: got %d want %d", got, bottom-step)
			}
		}
		m.Update(letter('j'))
		m.Update(tickMsg{})
		if m.viewport.YOffset() != bottom-4 {
			t.Fatal("j did not scroll down within the entry")
		}
		m.Update(letter('g'))
		m.Update(letter('g'))
		m.Update(letter('k'))
		if m.viewport.YOffset() != 0 {
			t.Fatal("k scrolled past top")
		}
		m.Update(letter('j'))
		if m.viewport.YOffset() != 1 {
			t.Fatal("j cannot scroll from top of a single entry")
		}
	}
}

func TestJKPreservesDraftAndArrowsStillSelectEntries(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.Update(letter('j'))
	m.Update(letter('k'))
	if m.input.Value() != "jk" {
		t.Fatal("compose mode intercepted navigation letters")
	}
	m.state.Active().Entries = []*app.Entry{{Role: "user", Text: "First"}, {Role: "assistant", Text: strings.Repeat("Long response\n\n", 40)}}
	m.refresh()
	m.setTranscriptFocus(true)
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.selected != 0 {
		t.Fatal("Up no longer selects previous entry")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.selected != 1 {
		t.Fatal("Down no longer selects next entry")
	}
	m.Update(letter('k'))
	if m.input.Value() != "jk" {
		t.Fatal("scrolling changed draft")
	}
}

func TestKeyboardScrollSelectsAndFoldsToolsAndThoughts(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := testModel(t)
		m.opts.Settings.VimNavigation, m.opts.Settings.Compact = true, compact
		m.state.Active().Entries = []*app.Entry{
			{ID: "intro", Role: "assistant", Text: "Intro"},
			{ID: "tool", Role: "tool", Tool: &app.ToolState{Title: "Read file", Status: "completed", Text: "TOOL DETAILS"}},
			{ID: "thinking", Role: "thought", Text: "THINKING DETAILS"},
			{ID: "response", Role: "assistant", Text: strings.Repeat("Response line\n\n", 40)},
		}
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		m.refresh()
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.Update(letter('g'))
		m.Update(letter('g'))
		// Navigate only with j/k, not arrows, while neighboring entries remain
		// visible. Enter must target the block crossed by the reading line.
		for range m.positions[1] {
			m.Update(letter('j'))
			m.Update(tickMsg{})
		}
		if m.selected != 1 {
			t.Fatal("scrolling past the tool header left selection behind")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m.Update(tickMsg{})
		if !strings.Contains(ansi.Strip(m.viewport.View()), "TOOL DETAILS") {
			t.Fatal("Enter did not expand the scrolled-to tool")
		}
		m.Update(letter(' '))
		m.Update(tickMsg{})
		if strings.Contains(ansi.Strip(m.viewport.GetContent()), "TOOL DETAILS") {
			t.Fatal("Space did not collapse tool")
		}
		for m.browseLine < m.positions[2] {
			m.Update(letter('j'))
			m.Update(tickMsg{})
		}
		if m.selected != 2 {
			t.Fatal("scrolling did not select thinking header")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m.Update(tickMsg{})
		if !strings.Contains(ansi.Strip(m.viewport.View()), "THINKING DETAILS") {
			t.Fatalf("Enter did not expand thinking compact=%v selected=%d line=%d offset=%d positions=%v expanded=%v:\n%s", compact, m.selected, m.browseLine, m.viewport.YOffset(), m.positions, m.expanded, ansi.Strip(m.viewport.GetContent()))
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m.Update(tickMsg{})
		if strings.Contains(ansi.Strip(m.viewport.GetContent()), "THINKING DETAILS") {
			t.Fatal("Enter did not collapse thinking")
		}
	}
}

func TestReadingLineCanSelectBlocksWhenViewportCannotScroll(t *testing.T) {
	m := testModel(t)
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "Short intro"}, {ID: "tool", Tool: &app.ToolState{Title: "Short tool", Text: "DETAILS"}}, {Role: "assistant", Text: "Short ending"}}
	m.refresh()
	m.setTranscriptFocus(true)
	m.handleKey(letter('g'))
	m.handleKey(letter('g'))
	for range m.positions[1] {
		m.Update(letter('j'))
		m.Update(tickMsg{})
	}
	if m.viewport.YOffset() != 0 || m.selected != 1 {
		t.Fatal("reading line cannot select a tool in a single-screen history")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(ansi.Strip(m.viewport.GetContent()), "DETAILS") {
		t.Fatal("could not open tool without scrolling viewport")
	}
}

func TestThoughtCanCollapseDespiteGlobalExpandPreference(t *testing.T) {
	m := testModel(t)
	m.opts.Settings.ShowThoughts = true
	m.state.Active().Entries = []*app.Entry{{ID: "thought", Role: "thought", Text: "REASONING BODY"}}
	m.refresh()
	m.setTranscriptFocus(true)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tickMsg{})
	if strings.Contains(ansi.Strip(m.viewport.GetContent()), "REASONING BODY") {
		t.Fatal("global preference prevented individual collapse")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tickMsg{})
	if !strings.Contains(ansi.Strip(m.viewport.GetContent()), "REASONING BODY") {
		t.Fatal("individual thought cannot reopen")
	}
}

func TestTranscriptSearchAndSemanticJumps(t *testing.T) {
	m := testModel(t)
	m.state.Active().Entries = []*app.Entry{
		{Role: "user", Text: "first turn"},
		{Role: "assistant", Text: "needle response"},
		{Role: "tool", Tool: &app.ToolState{Title: "needle tool", Status: "failed", Locations: []string{"src/file.go:5"}}},
		{Role: "user", Text: "second turn"},
		{Role: "tool", Tool: &app.ToolState{Title: "edit", Diffs: []harness.Diff{{Path: "changed.go", OldText: "before", NewText: "after"}}}},
	}
	m.openTranscriptSearch()
	m.list.Search.SetValue("needle")
	items := m.list.Filtered()
	if len(items) != 2 {
		t.Fatal("search omitted response/tool text")
	}
	m.choose(items[0])
	if m.selected != 1 || !m.focusedTranscript || m.searchQuery != "needle" {
		t.Fatal("search did not jump")
	}
	m.nextSearch(1)
	if m.selected != 2 {
		t.Fatal("n did not repeat search")
	}
	m.nextSearch(1)
	if m.selected != 1 {
		t.Fatal("search did not wrap")
	}
	m.jumpEntry(1, "turn")
	if m.selected != 3 {
		t.Fatal("turn jump failed")
	}
	m.jumpEntry(-1, "failed")
	if m.selected != 2 {
		t.Fatal("failed-tool jump failed")
	}
	m.jumpEntry(1, "edit")
	if m.selected != 4 {
		t.Fatal("edit jump failed")
	}
}

func TestSearchRevealsMatchInsideLongCollapsedTool(t *testing.T) {
	m := testModel(t)
	m.state.Active().Entries = []*app.Entry{{ID: "tool", Role: "tool", Tool: &app.ToolState{Title: "command", Text: strings.Repeat("output line\n", 50) + "needle at end"}}}
	m.searchQuery = "needle"
	m.setTranscriptFocus(true)
	m.revealSearch(0)
	if !m.expanded["tool"] || m.viewport.YOffset() == 0 || !strings.Contains(ansi.Strip(m.viewport.View()), "needle") {
		t.Fatal("search did not reveal buried tool output")
	}
}

func TestPermissionDetailsRemainScrollableWithoutApproval(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	i := &harness.Interaction{ID: "permission", Kind: "permission", Message: strings.Repeat("detail line\n", 100) + "FINAL DETAIL", Reply: make(chan harness.Decision, 1), Options: []harness.PermissionOption{{ID: "allow", Name: "Allow", Kind: "allow_once"}, {ID: "deny", Name: "Reject", Kind: "reject_once"}}}
	m.state.Interactions = []*harness.Interaction{i}
	m.syncInteraction()
	if !strings.Contains(m.info.GetContent(), "FINAL DETAIL") {
		t.Fatal("permission details truncated")
	}
	for range 30 {
		m.interactionKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !strings.Contains(ansi.Strip(m.info.View()), "FINAL DETAIL") {
		t.Fatal("cannot scroll permission details")
	}
	if item, _ := m.list.Selected(); item.ID != "deny" {
		t.Fatal("inspection changed safe default")
	}
	select {
	case <-i.Reply:
		t.Fatal("scrolling answered permission")
	default:
	}
}

func TestBusyEnterPreservesMultilineDraftAndReportsReason(t *testing.T) {
	m := testModel(t)
	m.state.Active().Busy = true
	m.input.SetValue("next task\nmore detail")
	if cmd := m.send(); cmd != nil || m.input.Value() != "next task\nmore detail" {
		t.Fatal("busy send consumed draft")
	}
	if !strings.Contains(m.state.Notice, "draft preserved") || !strings.Contains(ansi.Strip(m.feedback(100)), "draft preserved") {
		t.Fatal("busy send was silently ignored")
	}
}

func TestCodeBlocksAndReportedDiffInspection(t *testing.T) {
	m := testModel(t)
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "```go\nfmt.Println(\"hi\")\n```\n\n~~~~text\nraw\n~~~\n~~~~"}, {Role: "tool", Tool: &app.ToolState{Diffs: []harness.Diff{{Path: "file.go", OldText: "old\n", NewText: "new\n"}}}}}
	m.selected = 0
	m.openCodeBlocks()
	if len(m.list.Items) != 2 || m.list.Items[0].Value != "fmt.Println(\"hi\")" || m.list.Items[1].Value != "raw\n~~~" {
		t.Fatal("code block copy lost raw content")
	}
	m.openChanges()
	if m.overlay != "changes" || len(m.list.Items) != 1 {
		t.Fatal("changes are not navigable")
	}
	m.choose(m.list.Items[0])
	if m.overlay != "info" || len(m.infoHunks) == 0 || len(m.infoPaths) != 1 || !strings.Contains(m.infoTitle, "informational") {
		t.Fatal("diff inspection missing hunks or safety label")
	}
}

func TestReportedEditorPathsAreScopedAndLineAware(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file with spaces.go")
	if err := os.WriteFile(path, []byte("text"), 0600); err != nil {
		t.Fatal(err)
	}
	got, line, err := resolveReportedPath(root, "file with spaces.go:12")
	resolved, _ := filepath.EvalSymlinks(path)
	if err != nil || got != resolved || line != 12 {
		t.Fatalf("bad editor path: %q:%d %v", got, line, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, "."} {
		if _, _, err := resolveReportedPath(root, path); err == nil {
			t.Fatalf("unsafe editor target accepted: %q", path)
		}
	}
	t.Run("symlink escape", func(t *testing.T) {
		if err := os.Symlink(outside, filepath.Join(root, "escape.go")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, _, err := resolveReportedPath(root, "escape.go"); err == nil {
			t.Fatal("symlink escape accepted")
		}
	})
}

func TestFilePreviewIsBoundedAndStaleResultsIgnored(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.go"), []byte(strings.Repeat("a", 9000)), 0600); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.state.Active().Info.Workspace = root
	m.openFiles("")
	m.Update(filesMsg{files: []string{"file.go"}, generation: m.filePickerGeneration})
	m.filePreviewPath = ""
	msg := m.previewFile()().(filePreviewMsg)
	if len(msg.text) > 8300 || !strings.Contains(msg.text, "8 KiB") {
		t.Fatal("file preview not bounded")
	}
	m.Update(msg)
	if m.list.Items[0].Preview != msg.text {
		t.Fatal("preview not applied")
	}
	m.openFiles("")
	m.Update(filesMsg{files: []string{"file.go"}, generation: m.filePickerGeneration})
	m.Update(msg)
	if m.list.Items[0].Preview != "" {
		t.Fatal("stale preview leaked into new picker")
	}
}
