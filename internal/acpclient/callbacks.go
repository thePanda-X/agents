package acpclient

import (
	"context"
	"crypto/rand"
	"encoding/json"

	"agents/internal/clientservices"
	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func (c *Client) clientConfig() *acp.ClientConfig {
	return &acp.ClientConfig{
		Info: &acp.Implementation{Name: "agents", Title: acp.OptValue("Agents"), Version: "0.1.0"},
		Capabilities: &acp.ClientCapabilities{
			Auth:        acp.AuthCapabilities{Terminal: c.options.Launch.Command != ""},
			Fs:          acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal:    true,
			Elicitation: acp.OptValue(acp.ElicitationCapabilities{Form: acp.OptValue(acp.ElicitationFormCapabilities{}), URL: acp.OptValue(acp.ElicitationURLCapabilities{})}),
			Session:     acp.OptValue(acp.ClientSessionCapabilities{ConfigOptions: acp.OptValue(acp.SessionConfigOptionsCapabilities{Boolean: acp.OptValue(acp.BooleanConfigOptionCapabilities{})})}),
		},
		SessionUpdate: func(_ context.Context, notification *acp.SessionNotification) {
			event, err := normalizeUpdate(notification)
			if err != nil {
				c.sink(harness.Event{Kind: harness.Notice, Text: err.Error()})
				return
			}
			c.sink(event)
		},
		RequestPermission: c.requestPermission,
		ReadTextFile: func(_ context.Context, r *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
			text, err := c.files.Read(string(r.SessionID), r.Path, value(r.Line), value(r.Limit))
			if err != nil {
				return nil, err
			}
			return &acp.ReadTextFileResponse{Content: text}, nil
		},
		WriteTextFile: func(_ context.Context, r *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
			if err := c.files.Write(string(r.SessionID), r.Path, r.Content); err != nil {
				return nil, err
			}
			return &acp.WriteTextFileResponse{}, nil
		},
		Terminal: c.terminalHandlers(),
		Elicitation: &acp.ElicitationHandlers{Form: c.form, URL: c.url, Complete: func(_ context.Context, _ *acp.CompleteElicitationNotification) {
			c.sink(harness.Event{Kind: harness.Notice, Text: "External interaction completed."})
		}},
	}
}

func (c *Client) ask(ctx context.Context, interaction *harness.Interaction) harness.Decision {
	interaction.ID, interaction.Reply, interaction.Done = rand.Text(), make(chan harness.Decision, 1), ctx.Done()
	c.sink(harness.Event{Kind: harness.InteractionRequested, SessionID: interaction.SessionID, Interaction: interaction})
	defer c.sink(harness.Event{Kind: harness.InteractionFinished, ID: interaction.ID, SessionID: interaction.SessionID})
	select {
	case <-ctx.Done():
		return harness.Decision{Action: "cancel"}
	case decision := <-interaction.Reply:
		return decision
	}
}

func (c *Client) requestPermission(ctx context.Context, r *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	i := &harness.Interaction{Kind: "permission", SessionID: string(r.SessionID), Tool: toolPatch(r.ToolCall), Message: "The agent requests permission to use a tool."}
	for _, option := range r.Options {
		i.Options = append(i.Options, harness.PermissionOption{ID: string(option.OptionID), Name: option.Name, Kind: string(option.Kind)})
	}
	decision := c.ask(ctx, i)
	if decision.Action != "selected" {
		return &acp.RequestPermissionResponse{Outcome: &acp.RequestPermissionOutcomeCancelled{}}, nil
	}
	return &acp.RequestPermissionResponse{Outcome: &acp.SelectedPermissionOutcome{OptionID: acp.PermissionOptionID(decision.OptionID)}}, nil
}

func (c *Client) form(ctx context.Context, r *acp.CreateElicitationRequest, mode *acp.ElicitationFormMode) (*acp.CreateElicitationResponse, error) {
	schema, err := json.Marshal(mode.RequestedSchema)
	if err != nil {
		return nil, err
	}
	i := &harness.Interaction{Kind: "form", Message: r.Message, Schema: schema}
	if scope, ok := mode.Value.(*acp.ElicitationFormModeSession); ok {
		i.SessionID = string(scope.SessionID)
	}
	return decisionResponse(c.ask(ctx, i))
}

func (c *Client) url(ctx context.Context, r *acp.CreateElicitationRequest, mode *acp.ElicitationURLMode) (*acp.CreateElicitationResponse, error) {
	i := &harness.Interaction{Kind: "url", Message: r.Message, URL: mode.URL}
	if scope, ok := mode.Value.(*acp.ElicitationURLModeSession); ok {
		i.SessionID = string(scope.SessionID)
	}
	return decisionResponse(c.ask(ctx, i))
}

func exitStatus(status clientservices.ExitStatus) acp.TerminalExitStatus {
	if status.Signal != "" {
		return acp.TerminalExitStatus{Signal: acp.OptValue(status.Signal)}
	}
	return acp.TerminalExitStatus{ExitCode: acp.OptValue(uint32(status.Code))}
}

func (c *Client) terminalHandlers() *acp.TerminalHandlers {
	return &acp.TerminalHandlers{
		Create: func(_ context.Context, r *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
			env := make(map[string]string)
			for _, v := range r.Env {
				env[v.Name] = v.Value
			}
			id, err := c.terminals.Create(string(r.SessionID), r.Command, r.Args, env, value(r.Cwd), value(r.OutputByteLimit))
			if err != nil {
				return nil, err
			}
			return &acp.CreateTerminalResponse{TerminalID: acp.TerminalID(id)}, nil
		},
		Output: func(_ context.Context, r *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
			snapshot, err := c.terminals.Output(string(r.SessionID), string(r.TerminalID))
			if err != nil {
				return nil, err
			}
			response := &acp.TerminalOutputResponse{Output: snapshot.Output, Truncated: snapshot.Truncated}
			if snapshot.Exit != nil {
				response.ExitStatus = acp.OptValue(exitStatus(*snapshot.Exit))
			}
			return response, nil
		},
		WaitForExit: func(ctx context.Context, r *acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
			status, err := c.terminals.Wait(ctx, string(r.SessionID), string(r.TerminalID))
			if err != nil {
				return nil, err
			}
			exit := exitStatus(status)
			return &acp.WaitForTerminalExitResponse{ExitCode: exit.ExitCode, Signal: exit.Signal}, nil
		},
		Kill: func(_ context.Context, r *acp.KillTerminalRequest) (*acp.KillTerminalResponse, error) {
			if err := c.terminals.Kill(string(r.SessionID), string(r.TerminalID)); err != nil {
				return nil, err
			}
			return &acp.KillTerminalResponse{}, nil
		},
		Release: func(_ context.Context, r *acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
			if err := c.terminals.Release(string(r.SessionID), string(r.TerminalID)); err != nil {
				return nil, err
			}
			return &acp.ReleaseTerminalResponse{}, nil
		},
	}
}
