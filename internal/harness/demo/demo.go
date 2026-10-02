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
	return harness.Descriptor{ID: "demo", Name: "Demo", Description: "Local ACP test agent · no model calls or file edits", Factory: New}
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

func defaultOptions() []acp.SessionConfigOption {
	return []acp.SessionConfigOption{
		selectOption("model", "Model", "model", "Demo model", "Demo model", "Another model"),
		selectOption("effort", "Effort", "thought_level", "high", "low", "medium", "high"),
		selectOption("mode", "Mode", "mode", "build", "plan", "build"),
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
		response, err := session.CreateElicitation(ctx, &acp.CreateElicitationParams{Message: "How should I approach this refactoring?", Mode: &acp.ElicitationFormMode{RequestedSchema: acp.ElicitationSchema{Type: "object", Properties: map[string]acp.ElicitationPropertySchema{"strategy": &acp.StringPropertySchema{Title: acp.OptValue("Strategy"), Enum: acp.OptValue([]string{"conservative", "balanced", "aggressive"}), Default: acp.OptValue("balanced")}, "tests": &acp.BooleanPropertySchema{Title: acp.OptValue("Include tests"), Default: acp.OptValue(true)}}, Required: acp.OptValue([]string{"strategy", "tests"})}}})
		if ctx.Err() != nil {
			return cancelled()
		}
		if err != nil {
			return nil, err
		}
		_ = emit(chunk("assistant", fmt.Sprintf("Structured response received: `%T`. No files were changed.", response.Value)))
	} else {
		_ = emit(chunk("thought", "I'll keep the interface quiet, with details available when you need them."))
		_ = emit(&acp.Plan{Entries: []acp.PlanEntry{{Content: "Inspect the workspace", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted}, {Content: "Preview the configuration change", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted}}})
		_ = emit(&acp.ToolCall{ToolCallID: acp.ToolCallID(fmt.Sprintf("preview-%d", time.Now().UnixNano())), Title: "Preview configuration change · simulated", Kind: acp.OptValue(acp.ToolKindEdit), Status: acp.OptValue(acp.ToolCallStatusCompleted), Content: []acp.ToolCallContent{&acp.Diff{Path: "config.go", OldText: acp.OptValue("const accent = \"blue\"\n"), NewText: "const accent = \"pink\"\n"}}})
		answer := "## A calmer coding workspace\n\nOne interface for your coding agents, with **soft pink accents** and graphite surfaces.\n\n- Streamed responses, tools, and reviewable diffs.\n- Agent-provided models, effort, modes, and slash commands.\n- Permissions and structured questions without blocking the conversation.\n\n```go\nregistry.Register(opencode.Descriptor())\n```\n\nPress **Tab** to browse the transcript, **Ctrl+O** for session options, or **Ctrl+P** for commands. Try `/permission`, `/question`, or `/slow`.\n\n*This is a local ACP demo. No model was called and no files were changed.*"
		if strings.HasPrefix(text, "/slow") {
			answer = strings.Repeat("Streaming a cancellable response…\n\n", 40)
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
		_ = session.Update(ctx, &acp.SessionUpdateParams{Update: &acp.UsageUpdate{Used: 4200, Size: 200000}})
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}
