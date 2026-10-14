package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/storage"
	"agents/internal/tui/overlays"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func largePaste() string { return strings.Repeat("\tline with 界 and indentation\n", 45) }

func TestLargePasteInsertsAtCursorAndSendsFullTextOnce(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.input.Focus()
	m.input.SetValue("prefixsuffix")
	m.input.SetCursorColumn(len("prefix"))
	text := largePaste()
	m.Update(tea.PasteMsg{Content: text})
	pastes := m.currentDraft().Pastes
	if len(pastes) != 1 || m.input.Value() != "prefix"+pastes[0].Marker+"suffix" {
		t.Fatalf("paste was not compacted in place: %q", m.input.Value())
	}
	want := "prefix" + text + "suffix"
	if m.draftText() != want || m.submitting || len(adapter.prompts) != 0 {
		t.Fatal("pasting changed its content or sent a prompt")
	}
	m.Update(letter('!'))
	want = "prefix" + text + "!suffix"
	if m.draftText() != want {
		t.Fatal("cursor was not retained after the inserted block")
	}
	cmd := m.send()
	if cmd == nil {
		t.Fatal("collapsed draft could not be sent")
	}
	cmd()
	if len(adapter.prompts) != 1 || adapter.prompts[0].Text != want || m.input.Value() != "" {
		t.Fatal("send lost text, sent a marker, duplicated the prompt or retained the sent draft")
	}
	if m.currentDraft().Submission.Draft != want {
		t.Fatal("recovery snapshot retained presentation markers")
	}
}

func TestPasteThresholdsSmallTextAndNewlineNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		folded     bool
	}{
		{"small multiline", "first\nsecond", false},
		{"39 lines", strings.Repeat("x\n", 38) + "x", false},
		{"40 lines", strings.Repeat("x\n", 39) + "x", true},
		{"4095 bytes", strings.Repeat("x", 4095), false},
		{"4096 bytes", strings.Repeat("x", 4096), true},
		{"wide unicode", strings.Repeat("界", 1500), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			m.input.Focus()
			m.Update(tea.PasteMsg{Content: tc.text})
			if (len(m.currentDraft().Pastes) > 0) != tc.folded || m.draftText() != tc.text || m.submitting {
				t.Fatalf("wrong paste handling: folded=%v, display=%q", tc.folded, m.input.Value())
			}
		})
	}
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: strings.Repeat("\tline\r\n", 40)})
	if got := m.draftText(); got != strings.Repeat("\tline\n", 40) {
		t.Fatalf("large paste damaged indentation or CRLF: %q", got)
	}
}

func TestPasteLimitsRejectWholeBlockWithoutChangingDraft(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.input.SetValue("keep draft")
	for _, text := range []string{strings.Repeat("x", maxDraftBytes+1), "invalid \xff"} {
		m.Update(tea.PasteMsg{Content: text})
		if m.input.Value() != "keep draft" || len(m.currentDraft().Pastes) != 0 {
			t.Fatal("rejected paste changed the original draft")
		}
	}
	m.input.CharLimit = 12
	m.Update(tea.PasteMsg{Content: "would be truncated"})
	if m.input.Value() != "keep draft" {
		t.Fatal("composer limit silently truncated a paste")
	}
	m = testModel(t)
	m.input.Focus()
	m.Update(tea.PasteMsg{Content: strings.Repeat("x", maxDraftBytes)})
	if len(m.currentDraft().Pastes) != 1 {
		t.Fatal("exact draft limit should be accepted")
	}
	before := m.input.Value()
	m.Update(tea.PasteMsg{Content: "extra"})
	m.Update(letter('x'))
	if m.input.Value() != before || len(m.draftText()) != maxDraftBytes {
		t.Fatal("combined paste/edit limit damaged retained text")
	}
}

func TestPasteMarkersAreAtomicAndClearRestoreKeepsFullText(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.Update(tea.PasteMsg{Content: largePaste()})
	marker := m.input.Value()
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.input.Column() != 0 {
		t.Fatal("left navigation entered the marker")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.input.Column() != utf8.RuneCountInString(marker) {
		t.Fatal("right navigation entered the marker")
	}
	m.input.SetCursorColumn(5)
	m.Update(letter('!'))
	if m.input.Value() != marker+"!" || m.draftText() != largePaste()+"!" {
		t.Fatal("typing inside a marker edited the hidden block")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.input.Value() != "" || len(m.currentDraft().Pastes) != 0 {
		t.Fatal("backspace left a partial marker or retained an orphan block")
	}
	m.perform("restore_draft")
	if m.draftText() != largePaste() {
		t.Fatal("atomic deletion did not retain complete recovery text")
	}
	m.Update(ctrl('c'))
	m.perform("restore_draft")
	if m.draftText() != largePaste() {
		t.Fatal("clear/restore lost collapsed content")
	}
}

func TestPartialSelectionDeletesEntireMarker(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.Update(tea.PasteMsg{Content: largePaste()})
	m.input.SetCursorColumn(5)
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.input.Value() != "" || len(m.currentDraft().Pastes) != 0 {
		t.Fatal("partial selection left a broken marker")
	}
}

func TestPastedMarkerTextIsNotRecursivelyExpanded(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: largePaste()})
	firstMarker := m.input.Value()
	second := firstMarker + largePaste()
	m.Update(tea.PasteMsg{Content: second})
	if m.draftText() != largePaste()+second {
		t.Fatal("marker-shaped source text was recursively replaced")
	}
}

func TestPastesReviewAndRemovePreserveSurroundingDraft(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.input.Focus()
	m.input.SetValue("prefixsuffix")
	m.input.SetCursorColumn(6)
	m.Update(tea.PasteMsg{Content: largePaste()})
	want := m.draftText()
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt})
	if m.overlay != "pastes" || m.input.Value() == "" || m.draftText() != want {
		t.Fatal("paste review discarded the draft")
	}
	item, ok := m.list.Selected()
	if !ok {
		t.Fatal("missing paste item")
	}
	m.choose(item)
	if m.overlay != "info" || !strings.Contains(m.infoContent, "line with 界") {
		t.Fatal("paste inspection did not expose source text")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.overlay != "pastes" {
		t.Fatal("inspection did not return to paste list")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.input.Value() != "prefixsuffix" || len(m.currentDraft().Pastes) != 0 || len(adapter.prompts) != 0 {
		t.Fatal("removing a block changed surrounding text or sent a prompt")
	}
	if m.clearedDraft != want {
		t.Fatal("paste removal did not retain recovery text")
	}
}

func TestCollapsedDraftPersistsFullTextAndRestoresAcrossSessions(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: largePaste()})
	marker := m.input.Value()
	m.stashDraft()
	_, snapshot := m.Snapshot()
	key := storage.Key("/workspace", "demo")
	if got := snapshot.Workspaces[key].Drafts["s"]; got != largePaste() {
		t.Fatal("persisted draft contains a marker instead of its content")
	}
	m.newSession()
	if m.input.Value() != "" || len(m.currentDraft().Pastes) != 0 {
		t.Fatal("pasted content leaked into the new conversation")
	}
	m.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: "/workspace"}}}))
	if m.input.Value() != marker || m.draftText() != largePaste() {
		t.Fatal("session return lost the in-memory folded draft")
	}
	options := m.opts
	options.Snapshot, options.Restore = snapshot, &harness.SessionInfo{ID: "s", Workspace: "/workspace"}
	reopened := New(options)
	reopened.Update(eventMsg(harness.Event{Kind: harness.Connecting, Generation: 1}))
	reopened.Update(eventMsg(harness.Event{Kind: harness.Connected, Generation: 1}))
	reopened.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: *options.Restore}}))
	if reopened.draftText() != largePaste() || len(reopened.currentDraft().Pastes) != 1 {
		t.Fatal("restart did not restore the complete draft in compact form")
	}
}

func TestLargePasteRecoveryHistoryAndEditorUseActualText(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.Update(tea.PasteMsg{Content: largePaste()})
	m.send()()
	m.finishSubmission(true)
	m.recoverSubmission()
	if m.draftText() != largePaste() || len(adapter.prompts) != 1 {
		t.Fatal("submission recovery lost content or resent it")
	}
	m.input.Focus()
	m.navigateHistory(-1)
	if m.draftText() != largePaste() {
		t.Fatal("history restored a presentation marker")
	}
	m.Update(editorMsg{text: largePaste() + "edited"})
	if m.draftText() != largePaste()+"edited" || len(m.currentDraft().Pastes) != 1 {
		t.Fatal("editor result was truncated or discarded")
	}
	m.Update(editorMsg{err: errors.New("editor failed")})
	if m.draftText() != largePaste()+"edited" {
		t.Fatal("editor failure discarded the folded draft")
	}
}

func TestFirstSendCancellationPreservesPaste(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.newSession()
	m.Update(tea.PasteMsg{Content: largePaste()})
	create := m.send()
	if m.pendingFirstDraft != largePaste() || m.pendingFirstPrompt.Text != largePaste() {
		t.Fatal("first send captured markers")
	}
	m.Update(ctrl('c'))
	if m.pendingFirstPrompt != nil || m.draftText() != largePaste() {
		t.Fatal("cancelling queued send discarded pasted content")
	}
	create()
	m.Update(eventMsg(<-m.opts.Controller.Events))
	if len(adapter.prompts) != 0 || m.draftText() != largePaste() {
		t.Fatal("cancelled first send was sent or lost its draft")
	}
}

func TestEditingDuringFirstCreationDoesNotChangeCapturedPaste(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.newSession()
	m.Update(tea.PasteMsg{Content: largePaste()})
	create := m.send()
	m.input.SetValue("newer draft")
	m.Update(letter('x')) // also releases markers no longer in the composer
	create()
	_, followup := m.Update(eventMsg(<-m.opts.Controller.Events))
	followup().(tea.BatchMsg)[0]()
	if len(adapter.prompts) != 1 || adapter.prompts[0].Text != largePaste() || m.draftText() != "newer draftx" {
		t.Fatal("queued paste changed or discarded a newer draft")
	}
	if m.currentDraft().Submission.Draft != largePaste() {
		t.Fatal("queued paste recovery snapshot depended on deleted markers")
	}
}

func TestLargePasteNeverBecomesLocalSlashCommand(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	text := "/quit\n" + largePaste()
	m.Update(tea.PasteMsg{Content: text})
	cmd := m.send()
	if cmd == nil {
		t.Fatal("multiline slash text did not become a prompt")
	}
	cmd()
	if len(adapter.prompts) != 1 || adapter.prompts[0].Text != text {
		t.Fatal("folded multiline text ran a local slash command")
	}
}

func TestPasteRoutingAndLayoutRemainBounded(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := testModel(t)
		m.opts.Settings.Compact = compact
		m.Update(tea.PasteMsg{Content: largePaste()})
		for _, size := range [][2]int{{120, 40}, {40, 14}, {32, 12}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.refresh()
			if w, h := lipgloss.Size(m.View().Content); w > size[0] || h > size[1] {
				t.Fatalf("folded draft overflows terminal %v", size)
			}
			if m.draftText() != largePaste() {
				t.Fatal("resize damaged pasted content")
			}
		}
	}
	m := testModel(t)
	m.input.SetValue("keep draft")
	m.openList("test", "Filter", []overlays.Item{{ID: "one", Label: "one"}})
	m.Update(tea.PasteMsg{Content: "filter"})
	if m.input.Value() != "keep draft" || m.list.Search.Value() != "filter" {
		t.Fatal("picker paste reached the composer")
	}
	m.closeOverlay()
	m.setTranscriptFocus(true)
	m.Update(tea.PasteMsg{Content: largePaste()})
	if m.input.Value() != "keep draft" || len(m.currentDraft().Pastes) != 0 {
		t.Fatal("browse-mode paste changed the draft")
	}
}

func clipboardTextResult(m *Model, text string) clipboardTextMsg {
	before := m.input.Value()
	return clipboardTextMsg{text: text, before: before, selected: m.input.SelectedText(), caret: m.composerOffset(before),
		buffer: m.currentDraft(), generation: m.clipboardGeneration, bufferGeneration: m.currentDraft().Generation}
}

func TestClipboardTextUsesFoldingAndBlocksSendUntilComplete(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.input.SetValue("prefix")
	if m.pasteClipboardText() == nil || !m.pastingText || m.send() != nil {
		t.Fatal("text clipboard read was not guarded")
	}
	m.Update(clipboardTextResult(m, largePaste()))
	if m.pastingText || m.currentDraft().Pending != 0 || len(m.currentDraft().Pastes) != 1 || m.draftText() != "prefix"+largePaste() || len(adapter.prompts) != 0 {
		t.Fatal("text clipboard completion lost data, bypassed folding or sent a prompt")
	}
}

func TestClipboardTextIgnoresChangedDraftAndStaleConversation(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("original")
	m.pasteClipboardText()
	msg := clipboardTextResult(m, largePaste())
	m.input.SetValue("newer draft")
	m.Update(msg)
	if m.input.Value() != "newer draft" || len(m.currentDraft().Pastes) != 0 || m.pastingText {
		t.Fatal("late clipboard text overwrote newer input")
	}
	m.pasteClipboardText()
	msg = clipboardTextResult(m, largePaste())
	old := m.currentDraft()
	m.newSession()
	m.Update(msg)
	if m.input.Value() != "" || old.Pending != 0 || len(m.currentDraft().Pastes) != 0 {
		t.Fatal("clipboard text leaked into another conversation or left reads pending")
	}
}

func TestClipboardImageFallbackDoesNotPasteIntoNewerDraft(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.Images = true
	m.input.SetValue("original")
	m.pasteClipboardImage(true)
	msg := clipboardImageMsg{err: app.ErrNoClipboardImage, fallback: true, generation: m.clipboardGeneration,
		buffer: m.currentDraft(), bufferGeneration: m.currentDraft().Generation,
		before: m.input.Value(), caret: m.composerOffset(m.input.Value()), draftChecked: true}
	m.input.SetValue("newer draft")
	if cmd := m.receiveClipboardImage(msg); cmd != nil || m.pastingText || m.pastingImage || m.currentDraft().Pending != 0 {
		t.Fatal("image-to-text fallback ignored a changed draft")
	}
	if m.input.Value() != "newer draft" {
		t.Fatal("image-to-text fallback replaced newer input")
	}
}

func TestPastePreviewIsExplicitAndUTF8Safe(t *testing.T) {
	text := strings.Repeat("界", 30000)
	preview := pastePreview(text, 8192)
	if !utf8.ValidString(preview) || !strings.Contains(preview, "preview limited") || len(preview) >= len(text) {
		t.Fatal("preview cut a Unicode rune or silently omitted source content")
	}
}

func TestClosedSessionPersistsItsOwnPasteContent(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: largePaste()})
	m.Update(eventMsg(harness.Event{Kind: harness.SessionClosed, Generation: 1, SessionID: "s"}))
	_, snapshot := m.Snapshot()
	if got := snapshot.Workspaces[storage.Key("/workspace", "demo")].Drafts["s"]; got != largePaste() {
		t.Fatalf("closed session stored a marker or another draft: %q", got)
	}
}

func TestWhitespaceOnlyPasteDoesNotCreateSession(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.newSession()
	m.Update(tea.PasteMsg{Content: strings.Repeat(" ", 4096)})
	if m.send() != nil || adapter.newSessions != 0 || m.creatingSession {
		t.Fatal("folded whitespace became a nonempty native prompt")
	}
}

func TestPasteBlockCountIsBoundedWithoutDroppingEarlierText(t *testing.T) {
	m := testModel(t)
	for range maxPasteBlocks {
		m.Update(tea.PasteMsg{Content: largePaste()})
	}
	before := m.draftText()
	m.Update(tea.PasteMsg{Content: largePaste()})
	if len(m.currentDraft().Pastes) != maxPasteBlocks || m.draftText() != before || !strings.Contains(m.state.Notice, "Too many") {
		t.Fatal("block limit lost earlier content or silently accepted another block")
	}
}

func TestEditedDraftReadIsBoundedAndKeepsLargeSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.md")
	text := strings.Repeat("editor text\n", 20000)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := readEditedDraft(path)
	if msg.err != nil || msg.text != text {
		t.Fatal("large editor buffer was silently truncated")
	}
	m := testModel(t)
	m.Update(msg)
	if m.draftText() != text || len(m.currentDraft().Pastes) != 1 {
		t.Fatal("large editor result was not retained compactly")
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxDraftBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	msg = readEditedDraft(path)
	if msg.err == nil {
		t.Fatal("editor read accepted an oversized draft")
	}
	m.Update(msg)
	if m.draftText() != text {
		t.Fatal("oversized editor result replaced the previous draft")
	}
}
