package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Embedding the interface keeps this spy focused on operations these tests use.
// Every operation exercised below is implemented explicitly.
type commandAdapter struct {
	harness.Adapter
	prompts      []harness.Prompt
	optionValues []any
	cancelledID  string
	newSessions  int
}

func (*commandAdapter) Peer() harness.Peer { return harness.Peer{} }
func (*commandAdapter) Close() error       { return nil }
func (a *commandAdapter) NewSession(context.Context) (harness.Session, error) {
	a.newSessions++
	return harness.Session{SessionInfo: harness.SessionInfo{ID: "s", Workspace: "/workspace"}}, nil
}
func (a *commandAdapter) Prompt(_ context.Context, _ string, prompt harness.Prompt) (string, error) {
	a.prompts = append(a.prompts, prompt)
	return "end_turn", nil
}
func (a *commandAdapter) Cancel(_ context.Context, id string) error {
	a.cancelledID = id
	return nil
}
func (a *commandAdapter) SetOption(_ context.Context, _ string, option harness.ConfigOption, value any) ([]harness.ConfigOption, error) {
	a.optionValues = append(a.optionValues, value)
	option.Value = value
	return []harness.ConfigOption{option}, nil
}

func modelWithCommandAdapter(t *testing.T) (*Model, *commandAdapter) {
	t.Helper()
	m := testModel(t)
	adapter := &commandAdapter{}
	descriptor := harness.Descriptor{ID: "test", Name: "Test", Factory: func(context.Context, harness.Options, harness.Sink) (harness.Adapter, error) { return adapter, nil }}
	m.opts.Controller.Connect(descriptor, m.opts.Agent, nil)
	t.Cleanup(func() { _ = m.opts.Controller.Close() })
	for len(m.opts.Controller.Events) > 0 {
		<-m.opts.Controller.Events
	}
	return m, adapter
}

func TestLocalSlashCommandsNeverBecomePrompts(t *testing.T) {
	for _, name := range []string{"help", "commands", "options", "settings", "plan", "changes", "thoughts", "harness", "auth", "cancel"} {
		t.Run(name, func(t *testing.T) {
			m, adapter := modelWithCommandAdapter(t)
			m.attachments = []harness.Attachment{{Name: "notes.md"}}
			m.input.SetValue("/" + name)
			if cmd := m.send(); cmd != nil {
				cmd()
			}
			if len(adapter.prompts) != 0 || len(m.state.Active().Entries) != 0 || len(m.history) != 0 || m.submitting {
				t.Fatal("client command leaked into the prompt lifecycle")
			}
			if m.input.Value() != "" || len(m.attachments) != 1 {
				t.Fatal("client command was not consumed or discarded an attachment")
			}
		})
	}
}

func TestHelpWorksWithoutAnAgentConnection(t *testing.T) {
	m := testModel(t)
	m.state.ActiveID, m.state.Connection = "", "disconnected"
	m.input.SetValue("/help")
	m.send()
	if m.overlay != "info" || m.infoTitle == "" {
		t.Fatal("help requires an agent connection")
	}
	for _, name := range []string{"/model", "/cancel", "/agent"} {
		if !strings.Contains(slashHelp(), name) {
			t.Fatalf("help is missing %s", name)
		}
	}
}

func TestAgentAndUnknownSlashCommandsAreForwarded(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/compact summarize recent work", "/compact summarize recent work"},
		{"/permission", "/permission"},
		{"/unadvertised command arguments", "/unadvertised command arguments"},
		{"/agent help", "/help"},
		{"/agent /plan with arguments", "/plan with arguments"},
		{"/agent clear", "/clear"},
		{"/agent resume saved-session", "/resume saved-session"},
		{"/clear\nExplain this command", "/clear\nExplain this command"},
		{"/help\nExplain this command", "/help\nExplain this command"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			m, adapter := modelWithCommandAdapter(t)
			m.input.SetValue(tc.input)
			cmd := m.send()
			if cmd == nil {
				t.Fatal("agent command was intercepted")
			}
			cmd()
			if len(adapter.prompts) != 1 || adapter.prompts[0].Text != tc.want {
				t.Fatalf("forwarded prompts: %+v", adapter.prompts)
			}
		})
	}
}

func TestInvalidLocalCommandKeepsDraftAndDoesNotSend(t *testing.T) {
	for _, text := range []string{"/help unexpected", "/model missing", "/agent /", "/clear unexpected", "/models missing", "/remove file.go"} {
		m, adapter := modelWithCommandAdapter(t)
		m.input.SetValue(text)
		if cmd := m.send(); cmd != nil {
			cmd()
		}
		if m.state.LastError == nil || m.input.Value() != text || len(adapter.prompts) != 0 {
			t.Fatalf("invalid local command was lost or sent: %s", text)
		}
	}
}

func TestSlashModelUsesAdvertisedValuesAndSharedSelector(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	option := harness.ConfigOption{ID: "model", Name: "Model", Category: "model", Type: "select", Choices: []harness.Choice{{Value: "provider/model", Name: "Friendly Model"}}}
	m.state.Active().Options = []harness.ConfigOption{option}
	m.input.SetValue("/model")
	m.send()
	if m.overlay != "values" || m.option.ID != option.ID {
		t.Fatal("model command did not open the shared selector")
	}
	m.closeOverlay()
	m.input.SetValue("/model friendly model")
	cmd := m.send()
	if cmd == nil {
		t.Fatal("valid model was not set")
	}
	cmd()
	if !reflect.DeepEqual(adapter.optionValues, []any{"provider/model"}) || len(adapter.prompts) > 0 {
		t.Fatal("model command did not use the configuration API")
	}
}

func TestOptionChoiceRejectsAmbiguousDisplayNames(t *testing.T) {
	option := harness.ConfigOption{Name: "Model", Choices: []harness.Choice{{Value: "one/model", Name: "Model"}, {Value: "two/model", Name: "Model"}}}
	if _, err := matchOptionChoice(option, "model"); err == nil {
		t.Fatal("ambiguous display name selected a model")
	}
	if value, err := matchOptionChoice(option, "two/model"); err != nil || value != "two/model" {
		t.Fatal("exact model ID was not accepted")
	}
}

func TestSlashCancelAndNewReuseLifecycleActions(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Active().Busy = true
	m.input.SetValue("/new")
	m.send()
	if adapter.newSessions != 0 || m.sessionPending {
		t.Fatal("new session bypassed the active-turn guard")
	}
	m.input.SetValue("/cancel")
	cmd := m.send()
	if cmd == nil || !m.state.Active().Cancelling {
		t.Fatal("cancel did not enter the cancellation lifecycle")
	}
	cmd()
	if adapter.cancelledID != "s" {
		t.Fatal("cancel targeted the wrong session")
	}
}

func TestSlashPickerLabelsSourcesAndHandlesCollisions(t *testing.T) {
	m := testModel(t)
	m.state.Active().Commands = []harness.Command{{Name: "plan", Description: "Agent plan"}, {Name: "compact", Description: "Compact context", Hint: "[instructions]"}, {Name: "compact"}}
	items := m.slashItems(false)
	var clientPlan, agentPlan, compact int
	for _, item := range items {
		selection := item.Value.(slashSelection)
		switch item.ID {
		case "client:plan":
			clientPlan++
			if selection.Invocation != "/plan" || !strings.Contains(item.Detail, "Client") {
				t.Fatal("incorrect client item")
			}
		case "agent:plan":
			agentPlan++
			if selection.Invocation != "/agent plan" {
				t.Fatal("agent collision is not reachable")
			}
		case "agent:compact":
			compact++
			if selection.Invocation != "/compact" || !strings.Contains(item.Label, "[instructions]") {
				t.Fatal("agent hints were lost")
			}
		}
	}
	if clientPlan != 1 || agentPlan != 1 || compact != 1 {
		t.Fatal("command picker omitted or duplicated commands")
	}
}

func TestSlashCompletionPreservesArguments(t *testing.T) {
	for _, tc := range []struct{ input, invocation, want string }{
		{"/mo", "/model", "/model "},
		{"/mo provider/model", "/model", "/model provider/model"},
		{"/pl arguments", "/agent plan", "/agent plan arguments"},
		{"/agent co instructions", "/agent compact", "/agent compact instructions"},
		{"/mod provider/model", "/models", "/models provider/model"},
		{"/ad notes with spaces.md", "/add", "/add notes with spaces.md"},
	} {
		if got := completeSlashCommand(tc.input, tc.invocation); got != tc.want {
			t.Fatalf("complete(%q, %q) = %q, want %q", tc.input, tc.invocation, got, tc.want)
		}
	}
}

func TestTypingSlashOpensPickerAndEscapeKeepsTypedCommand(t *testing.T) {
	m := testModel(t)
	m.input.Focus()
	m.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.overlay != "commands" || len(m.list.Items) == 0 {
		t.Fatal("slash did not open completion")
	}
	m.list.Search.SetValue("custom-command")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.overlay != "" || strings.TrimSpace(m.input.Value()) != "/custom-command" {
		t.Fatal("dismissal discarded typed command")
	}
}

func TestDismissedSlashPickerDoesNotReopenWhileDeleting(t *testing.T) {
	for _, cancelKey := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}} {
		m := testModel(t)
		m.input.Focus()
		m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		m.Update(cancelKey)
		if m.overlay != "" || m.input.Value() != "/" {
			t.Fatal("cancel added completion whitespace or kept picker open")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		if m.overlay != "" || m.input.Value() != "" {
			t.Fatal("deleting cancelled slash reopened menu")
		}
		m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		if m.overlay != "commands" {
			t.Fatal("fresh slash no longer opens menu")
		}
		m.list.Search.SetValue("a")
		m.Update(cancelKey)
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		if m.overlay != "" || m.input.Value() != "/" {
			t.Fatal("backspacing command text to slash reopened menu")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		if m.overlay != "" || m.input.Value() != "" {
			t.Fatal("cannot remove cancelled slash")
		}
	}
}

func TestCommandSelectionRemainsVisibleThroughFullPicker(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 14}, {32, 12}} {
		m := testModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.openCommands()
		count := len(m.list.Items)
		for _, direction := range []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyUp}} {
			for range count {
				m.Update(direction)
				item, ok := m.list.Selected()
				if !ok {
					t.Fatal("picker lost selected option")
				}
				view := m.View().Content
				label := strings.Fields(item.Label)[0]
				if !strings.Contains(ansi.Strip(view), "› "+label) {
					t.Fatalf("selected %s not visible at %v cursor=%d:\n%s", label, size, m.list.Cursor, ansi.Strip(view))
				}
				if width, height := lipgloss.Size(view); width > size[0] || height > size[1] {
					t.Fatal("picker overflowed terminal")
				}
			}
		}
	}
}

func TestCommandPickerAllowsUnadvertisedCommands(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("/")
	m.openCommands()
	m.list.Search.SetValue("custom-command arguments")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.overlay != "" || strings.TrimSpace(m.input.Value()) != "/custom-command arguments" {
		t.Fatal("unknown command was trapped in completion")
	}
}

func TestSlashCommandPickerUpdatesWithAgentNotifications(t *testing.T) {
	m := testModel(t)
	m.openCommands()
	m.Update(eventMsg(harness.Event{Kind: harness.Commands, Generation: 1, SessionID: "s", Commands: []harness.Command{{Name: "compact"}}}))
	for _, item := range m.list.Items {
		if item.ID == "agent:compact" {
			return
		}
	}
	t.Fatal("advertised command update did not refresh the open picker")
}

func TestSlashAttachUsesLiteralWorkspacePathAndDoesNotSend(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	directory := t.TempDir()
	m.state.Active().Info.Workspace = directory
	path := filepath.Join(directory, "notes with spaces.md")
	if err := os.WriteFile(path, []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("/attach notes with spaces.md")
	cmd := m.send()
	if cmd == nil {
		t.Fatal("attach did not read the file")
	}
	msg := cmd().(attachedMsg)
	if msg.err != nil || msg.attachment.Path != path {
		t.Fatalf("attachment: %+v", msg)
	}
	m.Update(msg)
	if len(m.attachments) != 1 || len(adapter.prompts) != 0 {
		t.Fatal("attach sent a model prompt or lost the attachment")
	}
	m.input.SetValue("/detach")
	m.send()
	if len(m.attachments) != 0 {
		t.Fatal("detach did not remove attachments")
	}
}

func TestSlashCatalogNamesAreUniqueAndHaveHandlers(t *testing.T) {
	seen := make(map[string]bool)
	for _, local := range localSlashCommands {
		for _, name := range append([]string{local.Name}, local.Aliases...) {
			if name == "" || seen[name] || !strings.Contains(slashHelp(), "/"+name) {
				t.Fatalf("invalid or duplicate slash name: %s", name)
			}
			seen[name] = true
			resolved, ok := findLocalCommand(name)
			if !ok || resolved.Name != local.Name || resolved.Action != local.Action || resolved.Category != local.Category {
				t.Fatalf("alias %s does not resolve to %s", name, local.Name)
			}
		}
		if local.Action == "" && local.Category == "" && local.Name != "agent" {
			t.Fatalf("slash command has no handler: %s", local.Name)
		}
	}
}

func TestAliasesDispatchSharedActionsWithoutPrompts(t *testing.T) {
	for _, tc := range []struct{ input, overlay string }{
		{"/?", "info"}, {"/diff", "changes"}, {"/diffs", "changes"},
		{"/config", "settings"}, {"/prefs", "settings"}, {"/preferences", "settings"},
		{"/thinking", ""}, {"/reasoning", ""}, {"/find", "search"}, {"/login", "auth"},
		{"/resume", "sessions"}, {"/session", "sessions"}, {"/conversations", "sessions"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			m, adapter := modelWithCommandAdapter(t)
			m.state.Peer.Capabilities.List = true
			m.attachments = []harness.Attachment{{Name: "keep.md"}}
			m.input.SetValue(tc.input)
			m.send() // Do not run I/O commands: selectors should already be open.
			if m.overlay != tc.overlay || m.input.Value() != "" || m.submitting || len(adapter.prompts) != 0 || len(m.attachments) != 1 {
				t.Fatalf("alias not handled locally: %s", tc.input)
			}
		})
	}
}

func TestClearAndStopAliasesReuseLifecycleGuards(t *testing.T) {
	for _, alias := range []string{"clear", "reset", "new-chat"} {
		m, adapter := modelWithCommandAdapter(t)
		m.state.Active().Busy = true
		m.input.SetValue("/" + alias)
		if cmd := m.send(); cmd != nil {
			cmd()
		}
		if adapter.newSessions != 0 || m.sessionPending || len(adapter.prompts) != 0 {
			t.Fatal("new-conversation alias bypassed active-turn guard")
		}
		m.state.Active().Busy = false
		m.input.SetValue("/" + alias)
		if cmd := m.send(); cmd != nil {
			cmd()
		}
		if adapter.newSessions != 0 || m.state.Active() != nil || m.sessionPending {
			t.Fatal("clear alias did not open a local draft")
		}
	}
	for _, alias := range []string{"stop", "abort"} {
		m, adapter := modelWithCommandAdapter(t)
		m.state.Active().Busy = true
		m.input.SetValue("/" + alias)
		cmd := m.send()
		if cmd == nil || !m.state.Active().Cancelling {
			t.Fatal("stop alias bypassed cancellation lifecycle")
		}
		cmd()
		if adapter.cancelledID != "s" || len(adapter.prompts) != 0 {
			t.Fatal("stop alias did not cancel active session")
		}
	}
}

func TestModelsAliasUsesAdvertisedOptionValues(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.state.Active().Options = []harness.ConfigOption{{ID: "model", Category: "model", Name: "Model", Type: "select", Choices: []harness.Choice{{Value: "provider/model", Name: "Friendly Model"}}}}
	m.input.SetValue("/models Friendly Model")
	cmd := m.send()
	if cmd == nil {
		t.Fatal("models alias did not set model")
	}
	cmd()
	if !reflect.DeepEqual(adapter.optionValues, []any{"provider/model"}) || len(adapter.prompts) > 0 {
		t.Fatal("model alias did not use configuration API")
	}
}

func TestAttachAndRemoveAliasesPreserveLiteralPaths(t *testing.T) {
	for _, alias := range []string{"add", "file", "files"} {
		m, adapter := modelWithCommandAdapter(t)
		directory := t.TempDir()
		m.state.Active().Info.Workspace = directory
		path := filepath.Join(directory, "notes with spaces.md")
		if err := os.WriteFile(path, []byte("notes"), 0600); err != nil {
			t.Fatal(err)
		}
		m.input.SetValue("/" + alias + " notes with spaces.md")
		cmd := m.send()
		if cmd == nil {
			t.Fatal("attach alias did not read file")
		}
		msg := cmd().(attachedMsg)
		if msg.err != nil || msg.attachment.Path != path {
			t.Fatalf("bad literal attachment: %+v", msg)
		}
		m.Update(msg)
		m.input.SetValue("/remove")
		m.send()
		if len(m.attachments) != 0 || len(adapter.prompts) > 0 {
			t.Fatal("remove alias did not clear pending attachments locally")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal("remove alias deleted file")
		}
	}
}

func TestAliasCompletionAndAgentCollisionsAreDiscoverable(t *testing.T) {
	m := testModel(t)
	m.state.Active().Commands = []harness.Command{{Name: "clear", Description: "Harness clear"}, {Name: "resume", Description: "Harness resume"}}
	items := m.slashItems(false)
	var alias, collision bool
	for _, item := range items {
		if item.ID == "client:new" {
			alias = item.Value.(slashSelection).Invocation == "/new" && strings.Contains(item.Keywords, "clear") && strings.Contains(item.Preview, "/clear")
		}
		if item.ID == "agent:clear" {
			collision = item.Value.(slashSelection).Invocation == "/agent clear"
		}
	}
	if !alias || !collision {
		t.Fatal("alias or native command missing from completion")
	}
	for _, item := range m.slashItems(true) {
		if !strings.HasPrefix(item.ID, "agent:") || !strings.HasPrefix(item.Value.(slashSelection).Invocation, "/agent ") {
			t.Fatal("client alias leaked into agent-only picker")
		}
	}
	for _, name := range []string{"compact", "undo", "permissions", "logout"} {
		if _, ok := findLocalCommand(name); ok {
			t.Fatalf("unsafe equivalence reserved /%s", name)
		}
	}
}
