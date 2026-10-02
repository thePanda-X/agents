package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/harness/claude"
	"agents/internal/harness/demo"
	"agents/internal/storage"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	registry := harness.NewRegistry()
	if err := registry.Register(demo.Descriptor()); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Context: ctx, Controller: app.NewController(ctx), Registry: registry, Descriptor: demo.Descriptor(), Agent: harness.Options{Workspace: "/workspace"}, Settings: storage.Settings{Mouse: true}, Snapshot: storage.Snapshot{Version: 1, Workspaces: make(map[string]storage.WorkspaceState)}})
	m.state.Apply(harness.Event{Kind: harness.Connecting, Generation: 1})
	m.state.Apply(harness.Event{Kind: harness.Connected, Generation: 1})
	m.state.Apply(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: "/workspace"}}})
	return m
}

func TestResponsiveViewStaysInsideTerminal(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 14}, {24, 8}} {
		m := testModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.refresh()
		view := m.View().Content
		if width, height := lipgloss.Size(view); width > size[0] || height > size[1] {
			t.Fatalf("view %dx%d overflows terminal %dx%d", width, height, size[0], size[1])
		}
	}
}

func TestTranscriptRetainsScrollPositionDuringStreaming(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.state.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{ID: "reply", Role: "assistant", Text: strings.Repeat("A line of response.\n\n", 100)}})
	m.refresh()
	m.viewport.SetYOffset(10)
	m.state.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{ID: "reply", Role: "assistant", Text: "more"}})
	m.refresh()
	if m.viewport.YOffset() != 10 {
		t.Fatal("streaming moved the reader's scroll position")
	}
}

func TestPermissionDefaultsToRejectAndResponds(t *testing.T) {
	m := testModel(t)
	i := &harness.Interaction{ID: "permission", Kind: "permission", SessionID: "s", Reply: make(chan harness.Decision, 1), Options: []harness.PermissionOption{{ID: "allow", Name: "Allow", Kind: "allow_once"}, {ID: "deny", Name: "Reject", Kind: "reject_once"}}}
	m.state.Apply(harness.Event{Kind: harness.InteractionRequested, Generation: 1, Interaction: i})
	m.syncInteraction()
	item, ok := m.list.Selected()
	if !ok || item.ID != "deny" {
		t.Fatal("permission defaults to approval")
	}
	m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case decision := <-i.Reply:
		if decision.OptionID != "deny" {
			t.Fatal("wrong permission option")
		}
	default:
		t.Fatal("permission not answered")
	}
}

func TestRenderingIncludesCoreLayout(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m.state.Apply(harness.Event{Kind: harness.TurnStarted, Generation: 1, SessionID: "s", Text: "Explain the design", At: time.Date(2026, 9, 30, 10, 42, 0, 0, time.UTC)})
	m.state.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{Role: "assistant", Text: "## A calmer workspace\n\nSoft pink accents and graphite surfaces."}})
	m.refresh()
	view := ansi.Strip(m.View().Content)
	for _, text := range []string{"/workspace", "Explain the design", "calmer workspace", "Ask anything", "Ctrl+P"} {
		if !strings.Contains(view, text) {
			t.Fatalf("view missing %q:\n%s", text, view)
		}
	}
}

func TestExplicitKeyRemapWinsDefaultCollision(t *testing.T) {
	m := testModel(t)
	m.opts.Settings.Keybindings = map[string]string{"palette": "ctrl+s"}
	if m.actionFor("ctrl+s") != "palette" || m.actionFor("ctrl+p") != "" {
		t.Fatal("key remapping is ambiguous")
	}
}

func TestURLConsentRequiresFullInspectionAndRejectsUnsafeScheme(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	i := &harness.Interaction{ID: "url", Kind: "url", URL: "https://example.com/" + strings.Repeat("path/", 200), Reply: make(chan harness.Decision, 1)}
	m.state.Interactions = []*harness.Interaction{i}
	m.syncInteraction()
	if m.urlReviewed {
		t.Fatal("long URL considered inspected before scrolling")
	}
	m.list.Cursor = 1
	if cmd := m.interactionKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("URL opened before inspection")
	}
	msg := openURL(context.Background(), &harness.Interaction{URL: "file:///etc/passwd"})().(urlMsg)
	if msg.err == nil {
		t.Fatal("unsafe URL scheme accepted")
	}
}

func TestClearedDraftDoesNotReappearWhenReloaded(t *testing.T) {
	m := testModel(t)
	m.opts.Snapshot.Workspaces[storage.Key("/workspace", "demo")] = storage.WorkspaceState{Drafts: map[string]string{"s": "old draft"}}
	m.restoreDraft()
	if m.input.Value() != "old draft" {
		t.Fatal("initial draft was not restored")
	}
	m.input.Reset()
	m.Snapshot()
	m.restoreDraft()
	if m.input.Value() != "" {
		t.Fatal("cleared draft was restored from stale storage")
	}
}

func TestSessionLoadingDoesNotMoveAnotherConversationsDraft(t *testing.T) {
	m := testModel(t)
	m.displayedSessionID = "s"
	m.input.SetValue("keep draft in original conversation")
	m.Update(eventMsg(harness.Event{Kind: harness.SessionLoading, Generation: 1, SessionID: "another"}))
	if m.state.ActiveID != "s" {
		t.Fatal("pending load changed the active draft owner")
	}
	m.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "another", Workspace: "/workspace"}}}))
	if m.state.Conversations["s"].Draft != "keep draft in original conversation" || m.input.Value() != "" {
		t.Fatal("session load moved or lost the original draft")
	}
}

func TestReconnectDoesNotOverwriteDraftTypedDuringHandshake(t *testing.T) {
	m := testModel(t)
	m.displayedSessionID = "s"
	m.state.Active().Draft, m.state.Active().DraftLoaded = "before reconnect", true
	m.input.SetValue("typed during handshake")
	m.Update(eventMsg(harness.Event{Kind: harness.SessionReady, Generation: 1, Session: harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: "/workspace"}}}))
	if m.input.Value() != "typed during handshake" {
		t.Fatal("reconnect overwrote a live draft")
	}
}

func TestChoosingClaudeDoesNotTransferConversationContext(t *testing.T) {
	m := testModel(t)
	if err := m.opts.Registry.Register(claude.Descriptor()); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("draft belonging to the old harness")
	m.attachments = []harness.Attachment{{Name: "old.txt"}}
	m.state.Active().Commands = []harness.Command{{Name: "old-command"}}
	m.state.Apply(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{Role: "assistant", Text: "old reply"}})
	m.openHarnesses()
	var found bool
	for _, item := range m.list.Items {
		if item.ID == "claude" {
			if cmd := m.choose(item); cmd == nil {
				t.Fatal("choosing Claude did not schedule a connection")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Claude missing from the harness selector")
	}
	if m.opts.Descriptor.ID != "claude" || m.opts.Settings.Harness != "claude" || m.opts.Agent.Launch.Command != "claude-agent-acp" {
		t.Fatal("Claude selection did not configure its bridge")
	}
	if m.state.Active() != nil || m.input.Value() != "" || len(m.attachments) != 0 || m.opts.Demo {
		t.Fatal("switching harnesses transferred conversation state")
	}
	old := m.opts.Snapshot.Workspaces[storage.Key("/workspace", "demo")]
	if old.Drafts["s"] != "draft belonging to the old harness" {
		t.Fatal("switching harnesses discarded the old draft")
	}
	m.Update(eventMsg(harness.Event{Kind: harness.Message, Generation: 1, SessionID: "s", Chunk: harness.MessageChunk{Role: "assistant", Text: "late old reply"}}))
	if len(m.state.Conversations) != 0 {
		t.Fatal("old harness events leaked into Claude")
	}
}
