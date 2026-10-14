package acpclient_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"agents/internal/acpclient"
	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func lifecycleConfig() *acp.AgentConfig {
	return &acp.AgentConfig{
		NewSession: func(context.Context, *acp.AgentConn, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
			return &acp.NewSessionResponse{SessionID: "s"}, nil
		},
		Prompt: func(context.Context, *acp.AgentSession, *acp.PromptRequest) (*acp.PromptResponse, error) {
			return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
		},
		Cancel: func(context.Context, *acp.AgentSession, *acp.CancelNotification) {},
	}
}

func lifecycleClient(t *testing.T, config *acp.AgentConfig, options harness.Options) (*acpclient.Client, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	agent, err := acp.NewAgent(config)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, agentTransport := acp.NewInMemoryTransports()
	go func() { _ = agent.Run(ctx, agentTransport) }()
	client, err := acpclient.Connect(ctx, clientTransport, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, ctx
}

func TestKnownSessionLoadsWhenMetadataListingFails(t *testing.T) {
	for _, replay := range []bool{false, true} {
		config := lifecycleConfig()
		var loads atomic.Int32
		config.LoadSession = func(context.Context, *acp.AgentSession, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
			loads.Add(1)
			return &acp.LoadSessionResponse{}, nil
		}
		config.ResumeSession = func(context.Context, *acp.AgentSession, *acp.ResumeSessionRequest) (*acp.ResumeSessionResponse, error) {
			loads.Add(1)
			return &acp.ResumeSessionResponse{}, nil
		}
		config.ListSessions = func(context.Context, *acp.AgentConn, *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
			return nil, errors.New("listing temporarily unavailable")
		}
		workspace := t.TempDir()
		client, ctx := lifecycleClient(t, config, harness.Options{Workspace: workspace})
		info := harness.SessionInfo{ID: "s", Workspace: workspace, Title: "saved title", UpdatedAt: "2026-10-01T10:00:00Z"}
		session, err := client.LoadSession(ctx, info, replay)
		if err != nil || loads.Load() != 1 || session.ID != info.ID || session.Workspace != info.Workspace || session.Title != info.Title || session.UpdatedAt != info.UpdatedAt {
			t.Fatalf("known session could not load: %+v, loads=%d, err=%v", session, loads.Load(), err)
		}
	}
}

func TestSessionLookupDoesNotHideAuthErrorsOrGuessUnknownWorkspace(t *testing.T) {
	for _, auth := range []bool{false, true} {
		config := lifecycleConfig()
		var loads atomic.Int32
		config.LoadSession = func(context.Context, *acp.AgentSession, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
			loads.Add(1)
			return &acp.LoadSessionResponse{}, nil
		}
		config.ListSessions = func(context.Context, *acp.AgentConn, *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
			if auth {
				return nil, &acp.Error{Code: acp.ErrorCodeAuthenticationRequired, Message: "sign in"}
			}
			return nil, errors.New("listing failed")
		}
		workspace := t.TempDir()
		client, ctx := lifecycleClient(t, config, harness.Options{Workspace: workspace})
		info := harness.SessionInfo{ID: "s"}
		if auth {
			info.Workspace = workspace
		}
		_, err := client.LoadSession(ctx, info, true)
		if err == nil || loads.Load() != 0 || auth && !errors.Is(err, harness.ErrAuthRequired) {
			t.Fatalf("unsafe metadata fallback: loads=%d, err=%v", loads.Load(), err)
		}
	}
}

func TestSessionLookupStopsAtMatchingPage(t *testing.T) {
	config := lifecycleConfig()
	workspace := t.TempDir()
	var pages atomic.Int32
	config.ListSessions = func(context.Context, *acp.AgentConn, *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
		pages.Add(1)
		return &acp.ListSessionsResponse{Sessions: []acp.SessionInfo{{SessionID: "s", Cwd: workspace}}, NextCursor: acp.OptValue("more")}, nil
	}
	config.LoadSession = func(context.Context, *acp.AgentSession, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
		return &acp.LoadSessionResponse{}, nil
	}
	client, ctx := lifecycleClient(t, config, harness.Options{Workspace: workspace})
	if _, err := client.LoadSession(ctx, harness.SessionInfo{ID: "s"}, true); err != nil || pages.Load() != 1 {
		t.Fatalf("lookup scanned past its match: pages=%d, err=%v", pages.Load(), err)
	}
}

func TestCancelledMetadataLookupDoesNotLoadSavedSession(t *testing.T) {
	config := lifecycleConfig()
	started := make(chan struct{})
	var loads atomic.Int32
	config.ListSessions = func(ctx context.Context, _ *acp.AgentConn, _ *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	config.LoadSession = func(context.Context, *acp.AgentSession, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
		loads.Add(1)
		return &acp.LoadSessionResponse{}, nil
	}
	workspace := t.TempDir()
	client, lifetime := lifecycleClient(t, config, harness.Options{Workspace: workspace})
	ctx, cancel := context.WithCancel(lifetime)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := client.LoadSession(ctx, harness.SessionInfo{ID: "s", Workspace: workspace}, true)
		finished <- err
	}()
	select {
	case <-started:
		cancel()
	case <-lifetime.Done():
		t.Fatal("lookup did not start")
	}
	select {
	case err := <-finished:
		if err == nil || loads.Load() != 0 {
			t.Fatalf("cancelled lookup fell through to load: loads=%d, err=%v", loads.Load(), err)
		}
	case <-lifetime.Done():
		t.Fatal("cancelled lookup did not finish")
	}
}

func TestFailedLoadRevokesProvisionalFilesystemAccess(t *testing.T) {
	config := lifecycleConfig()
	workspace := t.TempDir()
	path := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(path, []byte("workspace text"), 0o600); err != nil {
		t.Fatal(err)
	}
	var native *acp.AgentSession
	config.LoadSession = func(ctx context.Context, session *acp.AgentSession, _ *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
		native = session
		if _, err := session.ReadTextFile(ctx, &acp.ReadTextFileParams{Path: path}); err != nil {
			return nil, err
		}
		return nil, errors.New("load failed after replay callbacks")
	}
	client, ctx := lifecycleClient(t, config, harness.Options{Workspace: workspace})
	if _, err := client.LoadSession(ctx, harness.SessionInfo{ID: "s", Workspace: workspace}, true); err == nil {
		t.Fatal("fixture did not fail")
	}
	if _, err := native.ReadTextFile(ctx, &acp.ReadTextFileParams{Path: path}); err == nil {
		t.Fatal("failed load retained filesystem authorization")
	}
}

func TestFailedReloadRestoresPreviousWorkspaceHandles(t *testing.T) {
	config := lifecycleConfig()
	old, replacement := t.TempDir(), t.TempDir()
	oldPath, newPath := filepath.Join(old, "old.txt"), filepath.Join(replacement, "new.txt")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var native *acp.AgentSession
	config.LoadSession = func(ctx context.Context, session *acp.AgentSession, _ *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
		native = session
		if _, err := session.ReadTextFile(ctx, &acp.ReadTextFileParams{Path: newPath}); err != nil {
			return nil, err
		}
		return nil, errors.New("reload failed")
	}
	client, ctx := lifecycleClient(t, config, harness.Options{Workspace: old})
	if _, err := client.NewSession(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.LoadSession(ctx, harness.SessionInfo{ID: "s", Workspace: replacement}, true); err == nil {
		t.Fatal("fixture did not fail")
	}
	if _, err := native.ReadTextFile(ctx, &acp.ReadTextFileParams{Path: oldPath}); err != nil {
		t.Fatalf("reload lost the original workspace: %v", err)
	}
	if _, err := native.ReadTextFile(ctx, &acp.ReadTextFileParams{Path: newPath}); err == nil {
		t.Fatal("failed reload retained replacement workspace")
	}
	if _, err := client.Prompt(ctx, "s", harness.Prompt{Text: "continue"}); err != nil {
		t.Fatalf("failed reload removed the existing session handle: %v", err)
	}
}

func TestDefaultModeFailureCleansSessionSetup(t *testing.T) {
	config, _ := modeAgentConfig("plan", []string{"plan", "build"}, true, nil)
	workspace := t.TempDir()
	var native *acp.AgentSession
	config.SetMode = func(_ context.Context, session *acp.AgentSession, _ *acp.SetSessionModeRequest) (*acp.SetSessionModeResponse, error) {
		native = session
		return nil, &acp.Error{Code: acp.ErrorCodeAuthenticationRequired, Message: "sign in"}
	}
	client, ctx := lifecycleClient(t, config, harness.Options{Workspace: workspace, DefaultModes: []string{"build"}})
	if _, err := client.NewSession(ctx); !errors.Is(err, harness.ErrAuthRequired) {
		t.Fatalf("mode error semantics changed: %v", err)
	}
	if _, err := native.WriteTextFile(ctx, &acp.WriteTextFileParams{Path: filepath.Join(workspace, "new.txt"), Content: "bad"}); err == nil {
		t.Fatal("failed setup retained write authorization")
	}
	if _, err := client.Prompt(ctx, "mode-fixture", harness.Prompt{Text: "must not send"}); err == nil {
		t.Fatal("failed setup retained a promptable client session")
	}
}

func TestCloseDuringSessionSetupDoesNotBlockFilesystemCleanup(t *testing.T) {
	config := lifecycleConfig()
	started := make(chan struct{})
	config.LoadSession = func(ctx context.Context, _ *acp.AgentSession, _ *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	workspace := t.TempDir()
	client, ctx := lifecycleClient(t, config, harness.Options{Workspace: workspace})
	finished := make(chan struct{})
	go func() {
		_, _ = client.LoadSession(ctx, harness.SessionInfo{ID: "s", Workspace: workspace}, true)
		close(finished)
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("session load did not begin")
	}
	closed := make(chan struct{})
	go func() { _ = client.Close(); close(closed) }()
	for _, done := range []chan struct{}{finished, closed} {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("shutdown blocked during session setup")
		}
	}
}

func TestFailedLoadCleansReplayTerminals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix command fixture")
	}
	config := lifecycleConfig()
	var terminal *acp.TerminalHandle
	config.LoadSession = func(ctx context.Context, session *acp.AgentSession, _ *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
		var err error
		terminal, _, err = session.CreateTerminal(ctx, &acp.CreateTerminalParams{Command: "sh", Args: []string{"-c", "sleep 60"}})
		if err != nil {
			return nil, err
		}
		return nil, errors.New("load failed")
	}
	workspace := t.TempDir()
	client, ctx := lifecycleClient(t, config, harness.Options{Workspace: workspace})
	if _, err := client.LoadSession(ctx, harness.SessionInfo{ID: "s", Workspace: workspace}, true); err == nil || terminal == nil {
		t.Fatalf("fixture did not create a terminal before failure: %v", err)
	}
	if _, err := terminal.Output(ctx, nil); err == nil {
		t.Fatal("failed replay retained its terminal")
	}
}
