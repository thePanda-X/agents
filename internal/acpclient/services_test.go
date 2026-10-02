package acpclient_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"agents/internal/acpclient"
	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func fixtureClient(t *testing.T, prompt func(context.Context, *acp.AgentSession, *acp.PromptRequest) (*acp.PromptResponse, error)) harness.Adapter {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	agent, err := acp.NewAgent(&acp.AgentConfig{
		NewSession: func(context.Context, *acp.AgentConn, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
			return &acp.NewSessionResponse{SessionID: "fixture"}, nil
		},
		Prompt: prompt,
		Cancel: func(context.Context, *acp.AgentSession, *acp.CancelNotification) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, agentTransport := acp.NewInMemoryTransports()
	go func() { _ = agent.Run(ctx, agentTransport) }()
	client, err := acpclient.Connect(ctx, clientTransport, harness.Options{Workspace: t.TempDir()}, func(harness.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestMinimalAgentCapabilitiesDegradeGracefully(t *testing.T) {
	client := fixtureClient(t, func(context.Context, *acp.AgentSession, *acp.PromptRequest) (*acp.PromptResponse, error) {
		return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Options) != 0 || client.Peer().Capabilities.List {
		t.Fatal("minimal agent gained unsupported features")
	}
	if _, err := client.ListSessions(ctx); !errors.Is(err, harness.ErrUnsupported) {
		t.Fatalf("unsupported operation: %v", err)
	}
	if _, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemCallbacksOverACP(t *testing.T) {
	var path string
	client := fixtureClient(t, func(ctx context.Context, session *acp.AgentSession, _ *acp.PromptRequest) (*acp.PromptResponse, error) {
		if _, err := session.WriteTextFile(ctx, &acp.WriteTextFileParams{Path: path, Content: "one\ntwo\n"}); err != nil {
			return nil, err
		}
		read, err := session.ReadTextFile(ctx, &acp.ReadTextFileParams{Path: path, Line: acp.OptValue(uint32(2)), Limit: acp.OptValue(uint32(1))})
		if err != nil {
			return nil, err
		}
		if read.Content != "two\n" {
			return nil, errors.New("incorrect ranged read")
		}
		return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(session.Workspace, "note.txt")
	if _, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "exercise filesystem"}); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalCallbacksOverACP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix command fixture")
	}
	client := fixtureClient(t, func(ctx context.Context, session *acp.AgentSession, _ *acp.PromptRequest) (*acp.PromptResponse, error) {
		terminal, _, err := session.CreateTerminal(ctx, &acp.CreateTerminalParams{Command: "sh", Args: []string{"-c", "printf test-output"}})
		if err != nil {
			return nil, err
		}
		defer terminal.Release(ctx, nil)
		if _, err := terminal.WaitForExit(ctx, nil); err != nil {
			return nil, err
		}
		output, err := terminal.Output(ctx, nil)
		if err != nil {
			return nil, err
		}
		if output.Output != "test-output" {
			return nil, errors.New("incorrect terminal output")
		}
		return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Prompt(ctx, session.ID, harness.Prompt{Text: "exercise terminal"}); err != nil {
		t.Fatal(err)
	}
}
