package acpclient_test

import (
	"context"
	"testing"
	"time"

	"agents/internal/acpclient"
	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func TestLoadRefreshesDateEvenWhenSavedReferenceHasTitle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const date = "2026-10-12T23:45:00.123456789-07:00"
	workspace := t.TempDir()
	agent, err := acp.NewAgent(&acp.AgentConfig{
		NewSession: func(context.Context, *acp.AgentConn, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
			return &acp.NewSessionResponse{SessionID: "s"}, nil
		},
		LoadSession: func(context.Context, *acp.AgentSession, *acp.LoadSessionRequest) (*acp.LoadSessionResponse, error) {
			return &acp.LoadSessionResponse{}, nil
		},
		ListSessions: func(context.Context, *acp.AgentConn, *acp.ListSessionsRequest) (*acp.ListSessionsResponse, error) {
			return &acp.ListSessionsResponse{Sessions: []acp.SessionInfo{{SessionID: "s", Cwd: workspace, Title: acp.OptValue("Native title"), UpdatedAt: acp.OptValue(date)}}}, nil
		},
		Prompt: func(context.Context, *acp.AgentSession, *acp.PromptRequest) (*acp.PromptResponse, error) {
			return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
		},
		Cancel: func(context.Context, *acp.AgentSession, *acp.CancelNotification) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, agentTransport := acp.NewInMemoryTransports()
	go func() { _ = agent.Run(ctx, agentTransport) }()
	client, err := acpclient.Connect(ctx, clientTransport, harness.Options{Workspace: workspace}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.LoadSession(ctx, harness.SessionInfo{ID: "s", Workspace: workspace, Title: "Saved title", UpdatedAt: "2026-10-01T10:00:00Z"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if session.UpdatedAt != date || session.Title != "Native title" {
		t.Fatalf("stale reference metadata was reused: %+v", session.SessionInfo)
	}
}
