package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/storage"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestModalCompositorCentersCompletePanels(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 14}, {32, 12}} {
		for _, kind := range []string{"settings", "palette", "commands", "info", "permission", "form", "url"} {
			t.Run(fmt.Sprintf("%s-%dx%d", kind, size[0], size[1]), func(t *testing.T) {
				m := testModel(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				switch kind {
				case "settings":
					m.openSettings()
				case "palette":
					m.Update(ctrl('p'))
				case "commands":
					m.input.Focus()
					m.Update(letter('/'))
				case "info":
					m.openInfo("Details", strings.Repeat("Detail line\n", 50))
				case "permission", "form", "url":
					i := &harness.Interaction{ID: kind, Kind: kind, Message: strings.Repeat("Inspect this detail.\n", 40), Reply: make(chan harness.Decision, 1)}
					if kind == "permission" {
						i.Options = []harness.PermissionOption{{ID: "yes", Name: "Allow once", Kind: "allow_once"}, {ID: "always", Name: "Allow always", Kind: "allow_always"}, {ID: "no", Name: "Reject", Kind: "reject_once"}, {ID: "never", Name: "Reject always", Kind: "reject_always"}}
					} else if kind == "form" {
						i.Schema = json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string","enum":["Yes","No"]}}}`)
					} else {
						i.URL = "https://example.com/" + strings.Repeat("path/", 100)
					}
					m.state.Interactions = []*harness.Interaction{i}
					m.syncInteraction()
				}
				m.resize()
				style, width, height := m.modalLayout()
				panel := style.Render(m.modalContent(width, height))
				pw, ph := lipgloss.Size(panel)
				if pw > size[0] || ph > size[1] {
					t.Fatalf("panel exceeds terminal: %dx%d in %v", pw, ph, size)
				}
				x, y := (size[0]-pw)/2, (size[1]-ph)/2
				view := m.View().Content
				rows, panelRows := strings.Split(view, "\n"), strings.Split(panel, "\n")
				for row, expected := range panelRows {
					if y+row >= len(rows) || ansi.Strip(ansi.Cut(rows[y+row], x, x+pw)) != ansi.Strip(expected) {
						t.Fatalf("panel row %d is not centered at %d,%d:\n%s", row, x, y, ansi.Strip(view))
					}
				}
				// Bubble Tea decomposes View.Content into terminal cells. Check the
				// resulting screen as well as string coordinates, so escape-code
				// handling cannot silently move a panel back to the origin.
				screen := lipgloss.NewCanvas(size[0], size[1])
				screen.Compose(lipgloss.NewLayer(view))
				for _, corner := range []struct {
					x, y  int
					glyph string
				}{{x, y, "╭"}, {x + pw - 1, y, "╮"}, {x, y + ph - 1, "╰"}, {x + pw - 1, y + ph - 1, "╯"}} {
					cell := screen.CellAt(corner.x, corner.y)
					if cell == nil || cell.Content != corner.glyph {
						t.Fatalf("terminal cell (%d,%d) does not contain centered panel corner %q", corner.x, corner.y, corner.glyph)
					}
				}
				if kind == "permission" {
					for _, label := range []string{"› Reject", "Enter confirm", "Esc cancel", "PgDn"} {
						if !strings.Contains(ansi.Strip(panel), label) {
							t.Fatalf("permission controls missing %q at %v:\n%s", label, size, ansi.Strip(panel))
						}
					}
				}
			})
		}
	}
}

func TestAttachmentReadStaysWithItsOriginalDraft(t *testing.T) {
	m := testModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	m.state.Active().Info.Workspace = root
	m.input.SetValue("Original task")
	read := m.attach("notes.md")
	if m.send() != nil || !strings.Contains(m.state.Notice, "Reading attachments") {
		t.Fatal("send raced the attachment read")
	}
	m.newSession()
	m.input.SetValue("New task")
	m.Update(read())
	if len(m.attachments) != 0 || m.input.Value() != "New task" {
		t.Fatal("late read changed the new conversation")
	}
	m.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: root}}}))
	if m.input.Value() != "Original task" || len(m.attachments) != 1 || m.attachments[0].Name != "notes.md" {
		t.Fatal("returning to the original conversation lost its complete draft")
	}
}

func TestDetachInvalidatesPendingFileReads(t *testing.T) {
	m := testModel(t)
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	read := m.attach(path)
	m.clearAttachments()
	m.Update(read())
	if len(m.attachments) != 0 || m.currentDraft().Pending != 0 {
		t.Fatal("a detached pending file was reattached")
	}
}

func TestLateClipboardImageRemainsWithOriginalConversation(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.Images = true
	m.input.SetValue("Screenshot task")
	m.pasteClipboardImage(false)
	buffer := m.currentDraft()
	msg := clipboardResult(m)
	msg.buffer, msg.bufferGeneration = buffer, buffer.Generation
	m.newSession()
	m.receiveClipboardImage(msg)
	if len(m.attachments) != 0 {
		t.Fatal("clipboard image landed in the new conversation")
	}
	m.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: "/workspace"}}}))
	if len(m.attachments) != 1 || !m.attachments[0].Image || m.input.Value() != "Screenshot task" {
		t.Fatal("clipboard image or text was lost in the original draft")
	}
}

func TestFailedSubmissionRecoveryProtectsNewDraftAndNeverResends(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.input.SetValue("Original prompt\nwith detail")
	m.attachments = []harness.Attachment{{Name: "notes.md"}}
	m.send() // Stage a submission, without executing transport I/O.
	m.Update(eventMsg(harness.Event{Kind: harness.TurnStarted, Generation: 1, SessionID: "s", Text: "Original prompt\nwith detail"}))
	m.input.SetValue("Newer draft")
	m.Update(eventMsg(harness.Event{Kind: harness.TurnFinished, Generation: 1, SessionID: "s", Err: errors.New("transport failed")}))
	if cmd := m.recoverSubmission(); cmd != nil || m.input.Value() != "Newer draft" {
		t.Fatal("recovery overwrote newer input")
	}
	m.input.Reset()
	m.recoverSubmission()
	if m.input.Value() != "Original prompt\nwith detail" || len(m.attachments) != 1 || len(adapter.prompts) != 0 {
		t.Fatal("recovery lost input, attachments, or automatically resent a prompt")
	}
	if !strings.Contains(m.state.Notice, "review") {
		t.Fatal("recovery did not explain uncertain acceptance")
	}
}

func TestSessionListFailureHasRetryAndIgnoresUnrelatedFailures(t *testing.T) {
	m := testModel(t)
	m.state.Peer.Capabilities.List = true
	m.openSessions()
	m.Update(eventMsg(harness.Event{Kind: harness.Failure, Generation: 1, Operation: harness.Config, Err: errors.New("config failed")}))
	if !m.list.Loading {
		t.Fatal("unrelated option failure stopped session listing")
	}
	m.Update(eventMsg(harness.Event{Kind: harness.Failure, Generation: 1, Operation: harness.SessionsListed, Err: errors.New("list failed")}))
	if m.list.Loading || m.list.Error == "" {
		t.Fatal("failed session listing stayed in a loading state")
	}
	if cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil || !m.list.Loading || m.list.Error != "" {
		t.Fatal("Enter did not retry session listing")
	}
}

func TestConfigurationCurrentValueBackNavigationAndPersistentSettings(t *testing.T) {
	m, _ := modelWithCommandAdapter(t)
	option := harness.ConfigOption{ID: "model", Name: "Model", Category: "model", Type: "select", Value: "two", Choices: []harness.Choice{{Value: "one", Name: "First"}, {Value: "two", Name: "Current"}}}
	m.state.Active().Options = []harness.ConfigOption{option}
	m.openOptions()
	m.choose(m.list.Items[0])
	item, _ := m.list.Selected()
	if item.ID != "two" || !item.Current {
		t.Fatal("model selector does not focus and mark the current value")
	}
	if cmd := m.choose(item); cmd != nil || m.overlay != "options" || m.configPending {
		t.Fatal("confirming the current value changed configuration or lost parent navigation")
	}
	m.choose(m.list.Items[0])
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.overlay != "options" {
		t.Fatal("Escape did not return to session options")
	}
	m.openSettings()
	m.list.Search.SetValue("motion")
	item, _ = m.list.Selected()
	m.choose(item)
	if m.overlay != "settings" || m.list.Search.Value() != "motion" || !m.opts.Settings.ReducedMotion {
		t.Fatal("setting toggle closed the panel or lost the filter")
	}
}

func TestIndividualAttachmentRemovalAndInspection(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("Keep the prompt")
	m.attachments = []harness.Attachment{{Name: "first.md"}, {Name: "second.md"}}
	m.openAttachments()
	m.choose(m.list.Items[0])
	if m.overlay != "info" || !strings.Contains(m.info.GetContent(), "first.md") {
		t.Fatal("attachment details did not expose the full name")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.overlay != "attachments" || len(m.attachments) != 1 || m.attachments[0].Name != "second.md" || m.input.Value() != "Keep the prompt" {
		t.Fatal("removing one attachment affected another attachment or the draft")
	}
}

func TestOperationalStatusAndTransientFeedbackAreIndependent(t *testing.T) {
	m := testModel(t)
	m.state.Active().Busy, m.state.Active().StartedAt = true, time.Now()
	m.state.LastError = errors.New("local attachment failed")
	if !strings.Contains(ansi.Strip(m.activity(40)), "Working") || !strings.Contains(ansi.Strip(m.activity(40)), "stop") || !strings.Contains(ansi.Strip(m.feedback(80)), "attachment failed") {
		t.Fatal("local error hid running state or cancellation guidance")
	}
	m.state.Active().Busy = false
	m.state.Active().StopReason = "cancelled"
	m.state.LastError = nil
	m.state.Notice = "Attached file.md"
	m.trackFeedback()
	if !strings.Contains(m.feedback(80), "Attached") || !strings.Contains(m.activity(80), "Ready") {
		t.Fatal("old cancellation state suppressed new feedback")
	}
	m.noticeAt = time.Now().Add(-9 * time.Second)
	if m.feedback(80) != "" {
		t.Fatal("transient notice did not expire")
	}
}

func TestTranscriptSearchStepsThroughOccurrencesInsideAnEntry(t *testing.T) {
	m := testModel(t)
	m.state.Active().Entries = []*app.Entry{{Role: "assistant", Text: "needle and needle\n\n" + strings.Repeat("filler\n\n", 30) + "needle"}, {Role: "assistant", Text: "another needle"}}
	m.searchQuery = "needle"
	m.setTranscriptFocus(true)
	m.revealSearch(0)
	if m.searchOccurrences != 3 || m.searchOccurrence != 0 {
		t.Fatal("search did not locate all occurrences in the response")
	}
	m.nextSearch(1)
	if m.selected != 0 || m.searchOccurrence != 1 {
		t.Fatal("n skipped the second occurrence on the same line")
	}
	m.nextSearch(1)
	if m.selected != 0 || m.searchOccurrence != 2 || m.viewport.YOffset() == 0 {
		t.Fatal("n did not reveal a later occurrence in the same response")
	}
	m.nextSearch(1)
	if m.selected != 1 {
		t.Fatal("n did not advance to the next matching entry")
	}
	m.nextSearch(-1)
	if m.selected != 0 || m.searchOccurrence != 2 {
		t.Fatal("N did not return to the previous entry's last occurrence")
	}
}

func TestReportedChangesGroupFilesWithoutMergingReports(t *testing.T) {
	m := testModel(t)
	m.state.Active().Entries = []*app.Entry{{Tool: &app.ToolState{Status: "completed", Diffs: []harness.Diff{{Path: "a.go", OldText: "one", NewText: "two"}}}}, {Tool: &app.ToolState{Status: "failed", Diffs: []harness.Diff{{Path: "a.go", OldText: "two", NewText: "three"}, {Path: "b.go", NewText: "new"}}}}}
	m.openChanges()
	if len(m.list.Items) != 2 || !strings.Contains(m.list.Description, "3 reports") {
		t.Fatal("changes overview did not summarize files and reports")
	}
	m.choose(m.list.Items[0])
	if m.overlay != "file_changes" || len(m.list.Items) != 2 || m.list.Items[1].Detail != "failed" {
		t.Fatal("repeated reports were merged or tool status was lost")
	}
	m.choose(m.list.Items[0])
	if !strings.Contains(m.info.GetContent(), "Reported tool status: completed") {
		t.Fatal("diff inspection did not identify the reported tool status")
	}
	m.backOverlay()
	if m.overlay != "file_changes" {
		t.Fatal("diff pager did not return to file reports")
	}
	m.backOverlay()
	if m.overlay != "changes" || len(m.list.Items) != 2 {
		t.Fatal("file reports did not return to the overview")
	}
}

func TestAppearancePreferencesPersistAndTerminalModeKeepsFocusCues(t *testing.T) {
	m := testModel(t)
	m.opts.Store = &storage.Store{Directory: t.TempDir()}
	m.opts.Settings.Appearance, m.opts.Settings.ReducedMotion = "terminal", true
	if result := m.save()().(saveMsg); result.err != nil {
		t.Fatal(result.err)
	}
	settings, _, err := m.opts.Store.Load()
	if err != nil || settings.Appearance != "terminal" || !settings.ReducedMotion {
		t.Fatal("accessibility preferences did not round trip")
	}
	m.openSettings()
	view := m.View()
	if strings.Contains(view.Content, "38;2;") || strings.Contains(view.Content, "48;2;") || view.BackgroundColor != nil || view.ForegroundColor != nil || !strings.Contains(ansi.Strip(view.Content), "› ") {
		t.Fatal("terminal-default appearance kept colors or lost keyboard focus cues")
	}
}

func TestNarrowFooterAndSessionRefreshPreserveEssentialInformation(t *testing.T) {
	m := testModel(t)
	footer := ansi.Strip(m.footer(28))
	if !strings.Contains(footer, "COMPOSE") || !strings.Contains(footer, "Enter") || !strings.Contains(footer, "Tab") {
		t.Fatalf("narrow footer lost essential hints: %s", footer)
	}
	m.state.Peer.Capabilities.List = true
	m.state.Sessions = []harness.SessionInfo{{ID: "s", Title: "Current", UpdatedAt: "2026-10-01T10:00:00Z"}, {ID: "other", Title: "Other", UpdatedAt: "2026-10-02T10:00:00Z"}}
	m.openSessions()
	m.populateSessions()
	selectListID(m.list, "s")
	m.populateSessions()
	item, _ := m.list.Selected()
	if item.ID != "s" || !item.Current || strings.Contains(item.Detail, "T10:") || strings.Contains(m.list.Description, "deletes") {
		t.Fatal("session refresh lost selection, active state, or capability-aware hints")
	}
}

func TestCommandProvenanceAndAliasSearchRemainDiscoverable(t *testing.T) {
	m := testModel(t)
	for _, text := range []string{"/settings", "/config", "/recover"} {
		m.input.SetValue(text)
		if !strings.Contains(ansi.Strip(m.footer(80)), "run local") {
			t.Fatalf("local provenance missing for %s", text)
		}
	}
	for _, text := range []string{"/agent settings", "/native-command"} {
		m.input.SetValue(text)
		if !strings.Contains(ansi.Strip(m.footer(80)), "send to Demo") {
			t.Fatalf("agent provenance missing for %s", text)
		}
	}
	m.openCommands()
	m.list.Search.SetValue("new-chat")
	items := m.list.Filtered()
	if len(items) != 1 || items[0].ID != "client:new" {
		t.Fatal("alias search did not find its canonical command")
	}
}
