package acpclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agents/internal/acpclient"
	"agents/internal/harness"
	"agents/internal/harness/claude"
	"agents/internal/harness/opencode"
	"agents/internal/harness/pi"

	"github.com/Tangerg/acp"
)

func modeAgentConfig(current string, choices []string, legacy bool, setErr error) (*acp.AgentConfig, *atomic.Int32) {
	var mu sync.Mutex
	var sets atomic.Int32
	catalog := func() (acp.Opt[[]acp.SessionConfigOption], acp.Opt[acp.SessionModeState]) {
		mu.Lock()
		defer mu.Unlock()
		if legacy {
			state := acp.SessionModeState{CurrentModeID: acp.SessionModeID(current)}
			for _, choice := range choices {
				state.AvailableModes = append(state.AvailableModes, acp.SessionMode{ID: acp.SessionModeID(choice), Name: choice})
			}
			return acp.Opt[[]acp.SessionConfigOption]{}, acp.OptValue(state)
		}
		selectOption := func(id, category, value string, values []string) acp.SessionConfigOption {
			options := acp.SessionConfigSelectOptionsUngrouped{}
			for _, v := range values {
				options = append(options, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueID(v), Name: v})
			}
			return acp.SessionConfigOption{ID: acp.SessionConfigID(id), Name: id, Category: acp.OptValue(acp.SessionConfigOptionCategory(category)), Value: &acp.SessionConfigSelect{CurrentValue: acp.SessionConfigValueID(value), Options: &options}}
		}
		options := []acp.SessionConfigOption{
			// A matching value in another category must not be treated as a mode.
			selectOption("model", "model", "plan", []string{"plan", "build", "bypassPermissions"}),
			selectOption("effort", "thought_level", "high", []string{"low", "high"}),
		}
		if len(choices) > 0 {
			options = append(options, selectOption("permission-mode", "mode", current, choices))
		}
		return acp.OptValue(options), acp.Opt[acp.SessionModeState]{}
	}
	set := func(value string) error {
		sets.Add(1)
		if setErr != nil {
			return setErr
		}
		mu.Lock()
		current = value
		mu.Unlock()
		return nil
	}
	config := &acp.AgentConfig{
		NewSession: func(context.Context, *acp.AgentConn, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
			options, modes := catalog()
			return &acp.NewSessionResponse{SessionID: "mode-fixture", ConfigOptions: options, Modes: modes}, nil
		},
		LoadSession: func(context.Context, *acp.AgentSession, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
			options, modes := catalog()
			return &acp.LoadSessionResponse{ConfigOptions: options, Modes: modes}, nil
		},
		ResumeSession: func(context.Context, *acp.AgentSession, *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
			options, modes := catalog()
			return &acp.ResumeSessionResponse{ConfigOptions: options, Modes: modes}, nil
		},
		Prompt: func(context.Context, *acp.AgentSession, *acp.PromptRequest) (*acp.PromptResponse, error) {
			return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
		},
		Cancel: func(context.Context, *acp.AgentSession, *acp.CancelNotification) {},
	}
	if legacy {
		config.SetMode = func(_ context.Context, _ *acp.AgentSession, request *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
			return &acp.SetSessionModeResponse{}, set(string(request.ModeID))
		}
	} else {
		config.SetConfigOption = func(_ context.Context, _ *acp.AgentSession, request *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
			if request.ConfigID != "permission-mode" {
				return nil, errors.New("only the mode may be changed")
			}
			value := request.Value.(*acp.SetSessionConfigOptionRequestValueID)
			if err := set(string(value.Value)); err != nil {
				return nil, err
			}
			options, _ := catalog()
			configs, _ := options.Get()
			return &acp.SetSessionConfigOptionResponse{ConfigOptions: configs}, nil
		}
	}
	return config, &sets
}

func modeClient(t *testing.T, options harness.Options, current string, choices []string, legacy bool, setErr error) (harness.Adapter, *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	config, sets := modeAgentConfig(current, choices, legacy, setErr)
	agent, err := acp.NewAgent(config)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, agentTransport := acp.NewInMemoryTransports()
	go func() { _ = agent.Run(ctx, agentTransport) }()
	options.Workspace = t.TempDir()
	client, err := acpclient.Connect(ctx, clientTransport, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, sets
}

func assertMode(t *testing.T, session harness.Session, want string) harness.ConfigOption {
	t.Helper()
	for _, option := range session.Options {
		if option.Category == "mode" {
			if option.Value != want {
				t.Fatalf("mode = %v, want %q", option.Value, want)
			}
			return option
		}
	}
	t.Fatal("missing mode option")
	return harness.ConfigOption{}
}

func TestSessionDefaultsSelectOnlyAdvertisedModes(t *testing.T) {
	for _, tt := range []struct {
		name, current, want string
		choices, defaults   []string
		sets                int32
	}{
		{"most permissive first", "plan", "bypassPermissions", []string{"plan", "acceptEdits", "bypassPermissions"}, []string{"bypassPermissions", "acceptEdits"}, 1},
		{"advertised fallback", "plan", "acceptEdits", []string{"plan", "acceptEdits"}, []string{"bypassPermissions", "acceptEdits"}, 1},
		{"already selected", "bypassPermissions", "bypassPermissions", []string{"plan", "bypassPermissions"}, []string{"bypassPermissions"}, 0},
		{"unavailable modes", "plan", "plan", []string{"plan"}, []string{"bypassPermissions", "acceptEdits"}, 0},
		{"custom defaults preserved", "plan", "plan", []string{"plan", "build"}, nil, 0},
	} {
		for _, legacy := range []bool{false, true} {
			name := tt.name
			if legacy {
				name += " legacy"
			}
			t.Run(name, func(t *testing.T) {
				client, sets := modeClient(t, harness.Options{DefaultModes: tt.defaults}, tt.current, tt.choices, legacy, nil)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				session, err := client.NewSession(ctx)
				if err != nil {
					t.Fatal(err)
				}
				assertMode(t, session, tt.want)
				if got := sets.Load(); got != tt.sets {
					t.Fatalf("mode updates = %d, want %d", got, tt.sets)
				}
				if !legacy && (len(session.Options) != 3 || session.Options[0].Value != "plan" || session.Options[1].Value != "high") {
					t.Fatalf("other controls changed: %+v", session.Options)
				}
			})
		}
	}
}

func TestSessionDefaultsReapplyOnLoadAndResumeButNotPrompt(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "config options"
		if legacy {
			name = "legacy modes"
		}
		t.Run(name, func(t *testing.T) {
			client, sets := modeClient(t, harness.Options{DefaultModes: []string{"build"}}, "plan", []string{"plan", "build"}, legacy, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := client.NewSession(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, replay := range []bool{true, false} {
				mode := assertMode(t, session, "build")
				if _, err := client.SetOption(ctx, session.ID, mode, "plan"); err != nil {
					t.Fatal(err)
				}
				before := sets.Load()
				if _, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "keep the selected mode"}); err != nil {
					t.Fatal(err)
				}
				if sets.Load() != before {
					t.Fatal("prompt overrode the user's mode")
				}
				session, err = client.LoadSession(ctx, session.SessionInfo, replay)
				if err != nil {
					t.Fatal(err)
				}
				assertMode(t, session, "build")
				if sets.Load() != before+1 {
					t.Fatal("load/resume did not reapply the default")
				}
			}
		})
	}
}

func TestSessionDefaultsDoNotInventModeControls(t *testing.T) {
	client, sets := modeClient(t, harness.Options{DefaultModes: []string{"build"}}, "plan", nil, false, nil)
	session, err := client.NewSession(context.Background())
	if err != nil || sets.Load() != 0 || len(session.Options) != 2 {
		t.Fatalf("invented mode control: session=%+v, sets=%d, error=%v", session, sets.Load(), err)
	}
}

func TestSessionDefaultModeFailureIsNotSilentlyIgnored(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		client, _ := modeClient(t, harness.Options{DefaultModes: []string{"build"}}, "plan", []string{"plan", "build"}, legacy, &acp.Error{Code: acp.ErrorCodeAuthenticationRequired, Message: "sign in first"})
		session, err := client.NewSession(context.Background())
		if !errors.Is(err, harness.ErrAuthRequired) || !strings.Contains(err.Error(), "set default mode") || session.ID != "" {
			t.Fatalf("mode failure lost: session=%+v, error=%v", session, err)
		}
	}
}

func TestHarnessFactoriesApplyDefaultModes(t *testing.T) {
	// The child process is this test binary, not an installed bridge or model.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", "")
	for _, tt := range []struct {
		descriptor harness.Descriptor
		want       string
	}{
		{opencode.Descriptor(), "build"},
		{claude.Descriptor(), "bypassPermissions"},
		{pi.Descriptor(), "plan"}, // No native permission mode is invented for Pi.
	} {
		t.Run(tt.descriptor.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			launch := harness.Launch{Command: executable, Args: []string{"-test.run=^TestHarnessDefaultHelperProcess$"}, Env: map[string]string{"AGENTS_TEST_DEFAULT_HARNESS": tt.descriptor.ID}}
			client, err := tt.descriptor.Factory(ctx, harness.Options{Workspace: t.TempDir(), Launch: launch}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			session, err := client.NewSession(ctx)
			if err != nil {
				t.Fatal(err)
			}
			assertMode(t, session, tt.want)
			if _, changed := launch.Env["OPENCODE_CONFIG_CONTENT"]; changed {
				t.Fatal("factory mutated the supplied launch environment")
			}
		})
	}
}

func TestHarnessDefaultHelperProcess(t *testing.T) {
	id := os.Getenv("AGENTS_TEST_DEFAULT_HARNESS")
	if id == "" {
		return
	}
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if id == "opencode" {
		var config struct {
			DefaultAgent string `json:"default_agent"`
			Permissions  []struct{ Action, Resource, Effect string }
		}
		if err := json.Unmarshal([]byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), &config); err != nil {
			fail(err)
		}
		if config.DefaultAgent != "build" || len(config.Permissions) != 1 || config.Permissions[0].Action != "*" || config.Permissions[0].Resource != "*" || config.Permissions[0].Effect != "allow" {
			fail(errors.New("OpenCode factory did not supply permissive V2 configuration"))
		}
	}
	config, _ := modeAgentConfig("plan", []string{"plan", "build", "acceptEdits", "bypassPermissions"}, false, nil)
	agent, err := acp.NewAgent(config)
	if err != nil {
		fail(err)
	}
	if err := agent.Run(context.Background(), acp.NewStdioTransport()); err != nil {
		fail(err)
	}
	// Do not write the Go test runner's PASS message to ACP stdout.
	os.Exit(0)
}
