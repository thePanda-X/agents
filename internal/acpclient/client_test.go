package acpclient_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"agents/internal/harness"
	"agents/internal/harness/claude"
	"agents/internal/harness/demo"
	"agents/internal/harness/opencode"
)

func demoClient(t *testing.T, sink harness.Sink) harness.Adapter {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client, err := demo.New(ctx, harness.Options{Workspace: t.TempDir()}, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestACPRoundTripConfigurationAndReplay(t *testing.T) {
	events := make(chan harness.Event, 1024)
	client := demoClient(t, func(e harness.Event) { events <- e })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Options) != 4 || session.ID == "" {
		t.Fatalf("missing session configuration: %+v", session)
	}
	options, err := client.SetOption(ctx, session.ID, session.Options[2], "plan")
	if err != nil || options[2].Value != "plan" {
		t.Fatalf("mode update: %+v, %v", options, err)
	}
	reason, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "show the interface"})
	if err != nil || reason != "end_turn" {
		t.Fatalf("prompt: %s, %v", reason, err)
	}
	var reply strings.Builder
	var tools, usage, commands bool
	for len(events) > 0 {
		e := <-events
		switch e.Kind {
		case harness.Message:
			if e.Chunk.Role == "assistant" {
				reply.WriteString(e.Chunk.Text)
			}
		case harness.Tool:
			tools = true
		case harness.UsageChanged:
			usage = true
		case harness.Commands:
			commands = true
		}
	}
	if !strings.Contains(reply.String(), "coding workspace") || !tools || !usage || !commands {
		t.Fatalf("incomplete event stream: %q tools=%v usage=%v commands=%v", reply.String(), tools, usage, commands)
	}
	sessions, err := client.ListSessions(ctx)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("session list: %+v, %v", sessions, err)
	}
	if _, err := client.LoadSession(ctx, sessions[0], true); err != nil {
		t.Fatal(err)
	}
	var replay strings.Builder
	for len(events) > 0 {
		e := <-events
		if e.Kind == harness.Message && e.Chunk.Role == "assistant" {
			replay.WriteString(e.Chunk.Text)
		}
	}
	if replay.String() != reply.String() {
		t.Fatal("history replay differs from original streamed response")
	}
	if err := client.CloseSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionResponseUsesOfferedOption(t *testing.T) {
	var offered bool
	client := demoClient(t, func(e harness.Event) {
		if e.Kind == harness.InteractionRequested {
			i := e.Interaction
			offered = len(i.Options) == 2 && i.Options[0].ID == "allow"
			i.Reply <- harness.Decision{Action: "selected", OptionID: "reject"}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reason, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "/permission"})
	if err != nil || reason != "end_turn" || !offered {
		t.Fatalf("permission roundtrip: %s, %v", reason, err)
	}
}

func TestCancelWhilePermissionIsPending(t *testing.T) {
	pending := make(chan *harness.Interaction, 1)
	client := demoClient(t, func(e harness.Event) {
		if e.Kind == harness.InteractionRequested {
			pending <- e.Interaction
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan string, 1)
	go func() {
		reason, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "/permission"})
		if err != nil {
			finished <- "error: " + err.Error()
		} else {
			finished <- reason
		}
	}()
	var interaction *harness.Interaction
	select {
	case interaction = <-pending:
	case <-ctx.Done():
		t.Fatal("permission was never requested")
	}
	if err := client.Cancel(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case reason := <-finished:
		if reason != "cancelled" {
			t.Fatalf("wrong stop reason: %s", reason)
		}
	case <-ctx.Done():
		t.Fatal("prompt did not finish after cancellation")
	}
	select {
	case <-interaction.Done:
	case <-ctx.Done():
		t.Fatal("pending permission handler was not cancelled")
	}
}

func TestStructuredFormRoundTrip(t *testing.T) {
	client := demoClient(t, func(e harness.Event) {
		if e.Kind == harness.InteractionRequested {
			e.Interaction.Reply <- harness.Decision{Action: "accept", Content: map[string]any{"strategy": "balanced", "tests": true}}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reason, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "/question"}); err != nil || reason != "end_turn" {
		t.Fatalf("form roundtrip: %s, %v", reason, err)
	}
}

// This opt-in test launches the real installed OpenCode but does not send a
// prompt or call a model. It creates and removes only its own empty test session.
func TestOpenCodeIntegration(t *testing.T) {
	if os.Getenv("OPENCODE_INTEGRATION") != "1" {
		t.Skip("set OPENCODE_INTEGRATION=1 to test the installed OpenCode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	descriptor := opencode.Descriptor()
	client, err := descriptor.Factory(ctx, harness.Options{Workspace: t.TempDir(), Launch: descriptor.Launch}, func(harness.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer := client.Peer()
	if peer.Name == "" {
		t.Fatal("agent did not identify itself")
	}
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if peer.Capabilities.Delete {
		defer client.DeleteSession(ctx, session.ID)
	}
	if len(session.Options) == 0 {
		t.Fatal("OpenCode did not return session options")
	}
	if peer.Capabilities.List {
		if _, err := client.ListSessions(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if peer.Capabilities.Close && peer.Capabilities.Load {
		if err := client.CloseSession(ctx, session.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := client.LoadSession(ctx, session.SessionInfo, true); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Connected to %s %s; %d session options; no model calls", peer.Name, peer.Version, len(session.Options))
}

// This opt-in test starts the real Claude ACP bridge and its SDK subprocess,
// but never authenticates, sends a prompt, or invokes a model. Session deletion
// is limited to the empty session this test creates in a temporary workspace.
func TestClaudeIntegration(t *testing.T) {
	if os.Getenv("CLAUDE_INTEGRATION") != "1" {
		t.Skip("set CLAUDE_INTEGRATION=1 to test the installed Claude ACP bridge")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	descriptor := claude.Descriptor()
	launch := descriptor.Launch
	if command := os.Getenv("CLAUDE_ACP_COMMAND"); command != "" {
		launch.Command = command
	}
	client, err := descriptor.Factory(ctx, harness.Options{Workspace: t.TempDir(), Launch: launch}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer := client.Peer()
	if peer.Name != "@agentclientprotocol/claude-agent-acp" && peer.Name != "@zed-industries/claude-agent-acp" {
		t.Fatalf("unexpected Claude bridge identity: %q", peer.Name)
	}
	for _, method := range peer.Auth {
		if !method.Terminal || len(method.Args) == 0 || method.Args[0] != "--cli" {
			t.Fatalf("unexpected Claude authentication method: %+v", method)
		}
	}
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if peer.Capabilities.Close {
			if err := client.CloseSession(cleanup, session.ID); err != nil {
				t.Errorf("close test session: %v", err)
			}
		}
		// Claude may not persist a transcript until the first prompt. Delete
		// only a listed record: deleting an unsaved empty session is an error.
		if peer.Capabilities.List && peer.Capabilities.Delete {
			sessions, err := client.ListSessions(cleanup)
			if err != nil {
				t.Errorf("list test workspace for cleanup: %v", err)
				return
			}
			for _, info := range sessions {
				if info.ID == session.ID {
					if err := client.DeleteSession(cleanup, session.ID); err != nil {
						t.Errorf("delete test session: %v", err)
					}
				}
			}
		}
	}()
	var model, mode bool
	for _, option := range session.Options {
		if option.Type == "select" && len(option.Choices) == 0 {
			t.Fatalf("Claude returned empty choices for %q", option.ID)
		}
		model = model || option.Category == "model"
		mode = mode || option.Category == "mode"
	}
	if !model || !mode {
		t.Fatalf("Claude did not return model and mode controls: %+v", session.Options)
	}
	for _, option := range session.Options {
		if option.Category != "model" && option.Category != "mode" && option.Category != "thought_level" {
			continue
		}
		options, err := client.SetOption(ctx, session.ID, option, option.Value)
		if err != nil {
			t.Fatalf("round-trip %s control: %v", option.ID, err)
		}
		var found bool
		for _, updated := range options {
			if updated.ID == option.ID && updated.Value == option.Value {
				found = true
			}
		}
		if !found {
			t.Fatalf("round-trip lost %s value: %+v", option.ID, options)
		}
	}
	if peer.Capabilities.List {
		if _, err := client.ListSessions(ctx); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Connected to %s %s; %d session options; %d auth methods; no model calls", peer.Name, peer.Version, len(session.Options), len(peer.Auth))
}
