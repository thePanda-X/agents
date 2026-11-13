// Package demo provides a deterministic in-memory ACP agent. It uses the same
// production transport boundary as OpenCode, without credentials or model calls.
package demo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"agents/internal/acpclient"
	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func Descriptor() harness.Descriptor {
	return harness.Descriptor{ID: "demo", Name: "Demo", Description: "Local ACP test agent · no model calls or file edits", Factory: New, PermissiveModes: []string{"bypass"}}
}

type saved struct {
	cwd     string
	options []acp.SessionConfigOption
	history []acp.SessionUpdate
}

type agentState struct {
	mu       sync.Mutex
	count    int
	sessions map[acp.SessionID]*saved
}

func New(ctx context.Context, options harness.Options, sink harness.Sink) (harness.Adapter, error) {
	state := &agentState{sessions: make(map[acp.SessionID]*saved)}
	agent, err := acp.NewAgent(&acp.AgentConfig{
		Info:         &acp.Implementation{Name: "demo", Version: "1.0.0"},
		NewSession:   state.newSession,
		Prompt:       state.prompt,
		Cancel:       func(context.Context, *acp.AgentSession, *acp.CancelNotification) {},
		LoadSession:  state.load,
		ListSessions: state.list,
		CloseSession: func(context.Context, *acp.AgentSession, *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
			return &acp.CloseSessionResponse{}, nil
		},
		DeleteSession: func(_ context.Context, s *acp.AgentSession, _ *acp.DeleteSessionRequest) (*acp.DeleteSessionResponse, error) {
			state.mu.Lock()
			delete(state.sessions, s.ID())
			state.mu.Unlock()
			return &acp.DeleteSessionResponse{}, nil
		},
		SetConfigOption: state.setOption,
	})
	if err != nil {
		return nil, err
	}
	clientTransport, agentTransport := acp.NewInMemoryTransports()
	go func() { _ = agent.Run(ctx, agentTransport) }()
	return acpclient.Connect(ctx, clientTransport, options, sink)
}

func selectOption(id, name, category, current string, names ...string) acp.SessionConfigOption {
	choices := acp.SessionConfigSelectOptionsUngrouped{}
	for _, name := range names {
		choices = append(choices, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueID(name), Name: name})
	}
	return acp.SessionConfigOption{ID: acp.SessionConfigID(id), Name: name, Category: acp.OptValue(acp.SessionConfigOptionCategory(category)), Value: &acp.SessionConfigSelect{CurrentValue: acp.SessionConfigValueID(current), Options: &choices}}
}

// modeOption is a select option whose choices have display names distinct
// from their values, as real harnesses advertise them.
func modeOption(current string, choices ...[2]string) acp.SessionConfigOption {
	options := acp.SessionConfigSelectOptionsUngrouped{}
	for _, choice := range choices {
		options = append(options, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueID(choice[0]), Name: choice[1]})
	}
	return acp.SessionConfigOption{ID: "mode", Name: "Mode", Category: acp.OptValue(acp.SessionConfigOptionCategory("mode")), Value: &acp.SessionConfigSelect{CurrentValue: acp.SessionConfigValueID(current), Options: &options}}
}

func defaultOptions() []acp.SessionConfigOption {
	return []acp.SessionConfigOption{
		selectOption("model", "Model", "model", "Demo model", "Demo model", "Another model"),
		selectOption("effort", "Effort", "thought_level", "high", "low", "medium", "high"),
		// Bypass is simulated: the demo never edits files or runs commands.
		// It shows how the composer flags a permissive mode (Shift+Tab).
		modeOption("build", [2]string{"plan", "Plan"}, [2]string{"build", "Build"}, [2]string{"bypass", "Bypass permissions"}),
		{ID: "verbose", Name: "Verbose output", Value: &acp.SessionConfigBoolean{}},
	}
}

func (a *agentState) newSession(_ context.Context, _ *acp.AgentConn, r *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.count++
	id := acp.SessionID(fmt.Sprintf("demo-%d", a.count))
	options := defaultOptions()
	a.sessions[id] = &saved{cwd: r.Cwd, options: options}
	return &acp.NewSessionResponse{SessionID: id, ConfigOptions: acp.OptValue(append([]acp.SessionConfigOption(nil), options...))}, nil
}

func (a *agentState) list(_ context.Context, _ *acp.AgentConn, _ *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	response := &acp.ListSessionsResponse{}
	for id, s := range a.sessions {
		response.Sessions = append(response.Sessions, acp.SessionInfo{SessionID: id, Cwd: s.cwd, Title: acp.OptValue("Demo conversation " + string(id))})
	}
	return response, nil
}

func (a *agentState) load(ctx context.Context, session *acp.AgentSession, _ *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
	a.mu.Lock()
	s, exists := a.sessions[session.ID()]
	if !exists {
		a.mu.Unlock()
		return nil, fmt.Errorf("unknown demo session")
	}
	history := append([]acp.SessionUpdate(nil), s.history...)
	options := append([]acp.SessionConfigOption(nil), s.options...)
	a.mu.Unlock()
	for _, update := range history {
		if err := session.Update(ctx, &acp.SessionUpdateParams{Update: update}); err != nil {
			return nil, err
		}
	}
	return &acp.LoadSessionResponse{ConfigOptions: acp.OptValue(options)}, nil
}

func (a *agentState) setOption(_ context.Context, session *acp.AgentSession, r *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[session.ID()]
	for i := range s.options {
		if s.options[i].ID != r.ConfigID {
			continue
		}
		switch v := r.Value.(type) {
		case *acp.SetSessionConfigOptionRequestValueID:
			option := *s.options[i].Value.(*acp.SessionConfigSelect)
			option.CurrentValue = v.Value
			s.options[i].Value = &option
		case *acp.SetSessionConfigOptionRequestBoolean:
			s.options[i].Value = &acp.SessionConfigBoolean{CurrentValue: v.Value}
		}
	}
	return &acp.SetSessionConfigOptionResponse{ConfigOptions: append([]acp.SessionConfigOption(nil), s.options...)}, nil
}

func (a *agentState) prompt(ctx context.Context, session *acp.AgentSession, r *acp.PromptRequest) (*acp.PromptResponse, error) {
	text := ""
	for _, block := range r.Prompt {
		if v, ok := block.(*acp.TextContent); ok {
			text += v.Text
		}
	}
	emit := func(update acp.SessionUpdate) error {
		a.mu.Lock()
		a.sessions[session.ID()].history = append(a.sessions[session.ID()].history, update)
		a.mu.Unlock()
		return session.Update(ctx, &acp.SessionUpdateParams{Update: update})
	}
	chunk := func(role, text string) acp.SessionUpdate {
		c := acp.ContentChunk{Content: &acp.TextContent{Text: text}}
		if role == "user" {
			return &acp.UserMessageChunk{ContentChunk: c}
		}
		if role == "thought" {
			return &acp.AgentThoughtChunk{ContentChunk: c}
		}
		return &acp.AgentMessageChunk{ContentChunk: c}
	}
	if err := emit(chunk("user", text)); err != nil {
		return nil, err
	}
	_ = session.Update(ctx, &acp.SessionUpdateParams{Update: &acp.AvailableCommandsUpdate{AvailableCommands: []acp.AvailableCommand{{Name: "permission", Description: "Try a permission request"}, {Name: "question", Description: "Try a structured question"}, {Name: "slow", Description: "Try cancellation"}}}})
	cancelled := func() (*acp.PromptResponse, error) {
		return &acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	}
	if strings.HasPrefix(text, "/permission") {
		response, err := session.RequestPermission(ctx, &acp.RequestPermissionParams{ToolCall: acp.ToolCallUpdate{ToolCallID: "demo-permission", Title: acp.OptValue("Simulated file edit (no files will change)")}, Options: []acp.PermissionOption{{OptionID: "allow", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce}, {OptionID: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce}}})
		if ctx.Err() != nil {
			return cancelled()
		}
		if err != nil {
			return nil, err
		}
		if _, ok := response.Outcome.(*acp.RequestPermissionOutcomeCancelled); ok {
			return cancelled()
		}
		_ = emit(chunk("assistant", "Your permission choice was received. No files were changed."))
	} else if strings.HasPrefix(text, "/question") {
		preview, _ := acp.NewMeta(map[string]any{"_claude/askUserQuestionOption": map[string]any{"preview": "Keep the public API stable.\n\n- Small, reviewable commits\n- Existing tests stay green"}})
		response, err := session.CreateElicitation(ctx, &acp.CreateElicitationParams{Message: "Please answer the following questions.", Mode: &acp.ElicitationFormMode{RequestedSchema: acp.ElicitationSchema{Type: "object", Properties: map[string]acp.ElicitationPropertySchema{
			"question_0": &acp.StringPropertySchema{
				Title:       acp.OptValue("Approach"),
				Description: acp.OptValue("How should I approach this refactoring?"),
				OneOf: acp.OptValue([]acp.EnumOption{
					{Const: "conservative", Title: "Conservative", Description: acp.OptValue("Preserve behavior and add tests first."), Meta: acp.OptValue(preview)},
					{Const: "balanced", Title: "Balanced", Description: acp.OptValue("Refactor the changed areas and leave the rest.")},
					{Const: "aggressive", Title: "Aggressive", Description: acp.OptValue("Restructure the package boundaries.")},
				}),
			},
			"question_0_custom": &acp.StringPropertySchema{Title: acp.OptValue("Other"), Description: acp.OptValue("Type your own answer, or add a note to the option you chose above (optional).")},
			"question_1": &acp.MultiSelectPropertySchema{
				Title:       acp.OptValue("Checks"),
				Description: acp.OptValue("Which checks should I run before handing off?"),
				Items: &acp.TitledMultiSelectItems{AnyOf: []acp.EnumOption{
					{Const: "unit tests", Title: "Unit tests", Description: acp.OptValue("go test ./...")},
					{Const: "race detector", Title: "Race detector", Description: acp.OptValue("go test -race ./...")},
					{Const: "demo", Title: "Interactive demo", Description: acp.OptValue("make demo for a manual walkthrough.")},
				}},
			},
			"question_1_custom": &acp.StringPropertySchema{Title: acp.OptValue("Other"), Description: acp.OptValue("Type your own answer to add to your selection above (optional).")},
		}}}})
		if ctx.Err() != nil {
			return cancelled()
		}
		if err != nil {
			return nil, err
		}
		_ = emit(chunk("assistant", fmt.Sprintf("Structured response received: `%T`. No files were changed.", response.Value)))
	} else {
		answer := demoAnswer
		if strings.HasPrefix(text, "/slow") {
			answer = strings.Repeat("Streaming a cancellable response…\n\n", 40)
		} else if !demoActivity(ctx, emit, chunk) {
			return cancelled()
		}
		for _, part := range strings.SplitAfter(answer, " ") {
			select {
			case <-ctx.Done():
				return cancelled()
			case <-time.After(12 * time.Millisecond):
			}
			if err := emit(chunk("assistant", part)); err != nil {
				if ctx.Err() != nil {
					return cancelled()
				}
				return nil, err
			}
		}
		// Above 80% of the window, so the header's context meter turns amber.
		_ = session.Update(ctx, &acp.SessionUpdateParams{Update: &acp.UsageUpdate{Used: 172400, Size: 200000}})
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// demoAnswer is the scripted response. Its table and inline code show the
// natural-width Markdown tables; the rows are a short tour of the interface.
const demoAnswer = "## A calmer coding workspace\n\n" +
	"This simulated turn shows the interface. **No model was called and no files were changed.**\n\n" +
	"| Try | What you will see |\n" +
	"| --- | --- |\n" +
	"| `Tab`, `↑`, `Enter` | Expand a tool; long output is capped |\n" +
	"| `o` while browsing | The complete tool output |\n" +
	"| `Ctrl+X` | Compact layout summarizes activity |\n" +
	"| `Shift+Tab` | Bypass mode, flagged in amber |\n" +
	"| `Ctrl+P` | Palette with recent actions |\n" +
	"| `/settings` | 24-hour message times |\n\n" +
	"The header's `ctx` meter turns amber above 80% of the context window, and `+3 −1` beside an edit summarizes its diff.\n\n" +
	"```go\nregistry.Register(opencode.Descriptor())\n```\n\n" +
	"Try `/permission`, `/question`, or `/slow` for approvals, forms, and cancellation."

// demoActivity streams simulated tool activity covering every tool state: a
// compact-groupable run (thinking, reads, a search, an edit with a diff), a
// command that runs long enough to show its spinner and then reports long
// output, and a failure. Nothing touches the workspace. It returns false when
// the turn is cancelled.
func demoActivity(ctx context.Context, emit func(acp.SessionUpdate) error, chunk func(role, text string) acp.SessionUpdate) bool {
	pause := func(d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
	prefix := fmt.Sprintf("demo-%d-", time.Now().UnixNano())
	tool := func(id, title string, kind acp.ToolKind, status acp.ToolCallStatus, content []acp.ToolCallContent, locations ...acp.ToolCallLocation) acp.SessionUpdate {
		return &acp.ToolCall{ToolCallID: acp.ToolCallID(prefix + id), Title: title + " · simulated", Kind: acp.OptValue(kind), Status: acp.OptValue(status), Content: content, Locations: locations}
	}
	text := func(value string) []acp.ToolCallContent {
		return []acp.ToolCallContent{&acp.Content{Content: &acp.TextContent{Text: value}}}
	}
	at := func(path string, line uint32) acp.ToolCallLocation {
		return acp.ToolCallLocation{Path: path, Line: acp.OptValue(line)}
	}
	var testOutput strings.Builder
	for _, name := range []string{"Banner", "Footer", "Palette", "Header", "Composer", "Diff", "Table", "Transcript", "Search", "Settings"} {
		for _, variant := range []string{"Wide", "Narrow", "Compact", "Comfortable"} {
			fmt.Fprintf(&testOutput, "=== RUN   Test%s%s\n--- PASS: Test%s%s (0.00s)\n", name, variant, name, variant)
		}
	}
	testOutput.WriteString("PASS\nok  \tagents/internal/tui\t0.412s")

	_ = emit(&acp.Plan{Entries: []acp.PlanEntry{
		{Content: "Inspect the interface code", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted},
		{Content: "Preview a configuration change", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted},
		{Content: "Run the checks", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted},
	}})
	steps := []struct {
		update acp.SessionUpdate
		wait   time.Duration
	}{
		{chunk("thought", "I'll look around before changing anything, and keep details available on demand."), 500 * time.Millisecond},
		{tool("read-view", "Read internal/tui/view.go", acp.ToolKindRead, acp.ToolCallStatusCompleted, nil, at("internal/tui/view.go", 1)), 150 * time.Millisecond},
		{tool("read-theme", "Read internal/tui/theme/theme.go", acp.ToolKindRead, acp.ToolCallStatusCompleted, nil, at("internal/tui/theme/theme.go", 1)), 150 * time.Millisecond},
		{tool("search", `Search for "composerMeta"`, acp.ToolKindSearch, acp.ToolCallStatusCompleted, text("internal/tui/view.go:301: func (m *Model) composerMeta() string\ninternal/tui/view.go:83: return frame.Render(m.input.View()) + \"\n\" + composerBottom(width, m.composerMeta(), border)")), 150 * time.Millisecond},
		{chunk("thought", "The accent and clock preference live in one place; a small edit covers both."), 400 * time.Millisecond},
		{tool("edit", "Edit config.go", acp.ToolKindEdit, acp.ToolCallStatusCompleted, []acp.ToolCallContent{&acp.Diff{
			Path:    "config.go",
			OldText: acp.OptValue("package config\n\nconst accent = \"blue\"\nconst density = \"comfortable\"\n"),
			NewText: "package config\n\nconst accent = \"pink\"\nconst density = \"comfortable\"\nconst clock = \"24h\"\nconst contextWarning = 0.8\n",
		}}, at("config.go", 3)), 150 * time.Millisecond},
		{tool("test", "go test ./internal/tui/...", acp.ToolKindExecute, acp.ToolCallStatusInProgress, nil), 1500 * time.Millisecond},
		{&acp.ToolCallUpdate{ToolCallID: acp.ToolCallID(prefix + "test"), Status: acp.OptValue(acp.ToolCallStatusCompleted), Content: acp.OptValue(text(testOutput.String()))}, 150 * time.Millisecond},
		{tool("vet", "go vet ./internal/demo", acp.ToolKindExecute, acp.ToolCallStatusFailed, text("internal/demo/demo.go:12:2: unreachable code\nexit status 1")), 200 * time.Millisecond},
	}
	for index, step := range steps {
		if err := emit(step.update); err != nil || !pause(step.wait) {
			return false
		}
		if index == 1 {
			// Report usage mid-turn so the header's context meter is visible
			// (and already amber) while tools are still running.
			_ = emit(&acp.UsageUpdate{Used: 164800, Size: 200000})
		}
	}
	return true
}
