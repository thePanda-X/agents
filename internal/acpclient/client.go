// Package acpclient implements the shared stable ACP v1 adapter. SDK details
// stay in this package and clientservices, never in the application or TUI.
package acpclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"agents/internal/clientservices"
	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

type Client struct {
	conn      *acp.ClientConn
	options   harness.Options
	sink      harness.Sink
	files     *clientservices.FileSystem
	terminals *clientservices.Terminals
	mu        sync.RWMutex
	sessions  map[string]*acp.ClientSession
	closeOnce sync.Once
	closeErr  error
}

var _ harness.Adapter = (*Client)(nil)

func New(ctx context.Context, options harness.Options, sink harness.Sink) (harness.Adapter, error) {
	path, err := exec.LookPath(options.Launch.Command)
	if err != nil {
		return nil, fmt.Errorf("find ACP executable %q: %w", options.Launch.Command, err)
	}
	cmd := exec.Command(path, options.Launch.Args...)
	cmd.Dir, cmd.Env = options.Workspace, clientservices.MergeEnv(os.Environ(), options.Launch.Env)
	cmd.Stderr = options.Diagnostics
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	return Connect(ctx, acp.NewCommandTransport(&acp.CommandConfig{Command: cmd, TerminationGrace: 2 * time.Second}), options, sink)
}

// Connect also accepts in-memory transports, allowing protocol integration tests
// to exercise the production adapter without an external agent or model call.
func Connect(ctx context.Context, transport acp.Transport, options harness.Options, sink harness.Sink) (*Client, error) {
	if sink == nil {
		sink = func(harness.Event) {}
	}
	c := &Client{options: options, sink: sink, files: clientservices.NewFileSystem(), sessions: make(map[string]*acp.ClientSession)}
	c.terminals = clientservices.NewTerminals(c.files, func(sessionID, id, output string) {
		sink(harness.Event{Kind: harness.TerminalOutput, SessionID: sessionID, ID: id, Text: output})
	})
	config := c.clientConfig()
	client, err := acp.NewClient(config)
	if err != nil {
		c.files.Close()
		return nil, err
	}
	handshake, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c.conn, err = client.Connect(handshake, transport)
	if err != nil {
		c.terminals.Close()
		c.files.Close()
		return nil, fmt.Errorf("ACP initialization: %w", err)
	}
	go func() {
		err := c.conn.Wait()
		sink(harness.Event{Kind: harness.Disconnected, Err: err})
	}()
	return c, nil
}

func (c *Client) Peer() harness.Peer {
	peer := c.conn.Peer()
	caps := peer.AgentCapabilities
	result := harness.Peer{Capabilities: harness.Capabilities{
		Load:            caps.LoadSession,
		Resume:          supported(caps.SessionCapabilities.Resume),
		List:            supported(caps.SessionCapabilities.List),
		Close:           supported(caps.SessionCapabilities.Close),
		Delete:          supported(caps.SessionCapabilities.Delete),
		Images:          caps.PromptCapabilities.Image,
		EmbeddedContext: caps.PromptCapabilities.EmbeddedContext,
		HTTPMCP:         caps.McpCapabilities.HTTP,
		AdditionalRoots: supported(caps.SessionCapabilities.AdditionalDirectories),
	}}
	if info, ok := peer.AgentInfo.Get(); ok {
		result.Name, result.Version = info.Name, info.Version
	}
	for _, auth := range peer.AuthMethods {
		switch v := auth.(type) {
		case *acp.AuthMethodAgent:
			result.Auth = append(result.Auth, harness.AuthMethod{ID: string(v.ID), Name: v.Name, Description: value(v.Description)})
		case *acp.AuthMethodTerminal:
			result.Auth = append(result.Auth, harness.AuthMethod{ID: string(v.ID), Name: v.Name, Description: value(v.Description), Terminal: true, Args: v.Args, Env: value(v.Env)})
		}
	}
	return result
}

func (c *Client) mcp() ([]acp.McpServer, error) {
	var servers []acp.McpServer
	if len(c.options.MCP) > 0 {
		if err := json.Unmarshal(c.options.MCP, &servers); err != nil {
			return nil, fmt.Errorf("MCP configuration: %w", err)
		}
	}
	if servers == nil {
		servers = []acp.McpServer{}
	}
	return servers, nil
}

func (c *Client) NewSession(ctx context.Context) (harness.Session, error) {
	servers, err := c.mcp()
	if err != nil {
		return harness.Session{}, err
	}
	session, response, err := c.conn.NewSession(ctx, &acp.NewSessionRequest{Cwd: c.options.Workspace, AdditionalDirectories: c.options.AdditionalRoots, McpServers: servers})
	if err != nil {
		return harness.Session{}, mapError(err)
	}
	info := harness.SessionInfo{ID: string(session.ID()), Workspace: c.options.Workspace, AdditionalRoots: c.options.AdditionalRoots}
	if err := c.register(session, info); err != nil {
		return harness.Session{}, err
	}
	options, err := normalizeOptions(response.ConfigOptions, response.Modes)
	return harness.Session{SessionInfo: info, Options: options}, err
}

func (c *Client) LoadSession(ctx context.Context, info harness.SessionInfo, replay bool) (harness.Session, error) {
	caps := c.Peer().Capabilities
	if (replay && !caps.Load) || (!replay && !caps.Resume) {
		return harness.Session{}, harness.ErrUnsupported
	}
	// An explicit --session may arrive with only an ID. Discover the saved
	// directory instead of assuming the client's current workspace is its home.
	if info.Title == "" && caps.List {
		stored, err := c.listSessions(ctx, acp.Opt[string]{})
		if err != nil {
			return harness.Session{}, err
		}
		for _, candidate := range stored {
			if candidate.ID == info.ID {
				info.Workspace, info.Title, info.UpdatedAt = candidate.Workspace, candidate.Title, candidate.UpdatedAt
				break
			}
		}
	}
	servers, err := c.mcp()
	if err != nil {
		return harness.Session{}, err
	}
	if info.Workspace == "" {
		info.Workspace = c.options.Workspace
	}
	if info.AdditionalRoots == nil {
		info.AdditionalRoots = c.options.AdditionalRoots
	}
	// Register roots first: replay can contain client callbacks before the
	// lifecycle response returns. The selected session's workspace is authoritative.
	if err := c.files.SetSession(info.ID, append([]string{info.Workspace}, info.AdditionalRoots...)); err != nil {
		return harness.Session{}, err
	}
	var session *acp.ClientSession
	var options []harness.ConfigOption
	if replay {
		var response *acp.LoadSessionResponse
		session, response, err = c.conn.LoadSession(ctx, &acp.LoadSessionRequest{SessionID: acp.SessionID(info.ID), Cwd: info.Workspace, AdditionalDirectories: info.AdditionalRoots, McpServers: servers})
		if err == nil {
			options, err = normalizeOptions(response.ConfigOptions, response.Modes)
		}
	} else {
		var response *acp.ResumeSessionResponse
		session, response, err = c.conn.ResumeSession(ctx, &acp.ResumeSessionRequest{SessionID: acp.SessionID(info.ID), Cwd: info.Workspace, AdditionalDirectories: info.AdditionalRoots, McpServers: servers})
		if err == nil {
			options, err = normalizeOptions(response.ConfigOptions, response.Modes)
		}
	}
	if err != nil {
		return harness.Session{}, mapError(err)
	}
	if err := c.register(session, info); err != nil {
		return harness.Session{}, err
	}
	return harness.Session{SessionInfo: info, Options: options}, nil
}

func (c *Client) register(session *acp.ClientSession, info harness.SessionInfo) error {
	if err := c.files.SetSession(info.ID, append([]string{info.Workspace}, info.AdditionalRoots...)); err != nil {
		return err
	}
	c.mu.Lock()
	c.sessions[info.ID] = session
	c.mu.Unlock()
	return nil
}

func (c *Client) session(id string) (*acp.ClientSession, error) {
	c.mu.RLock()
	session := c.sessions[id]
	c.mu.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("session %q is not open on this connection", id)
	}
	return session, nil
}

func (c *Client) ListSessions(ctx context.Context) ([]harness.SessionInfo, error) {
	if !c.Peer().Capabilities.List {
		return nil, harness.ErrUnsupported
	}
	return c.listSessions(ctx, acp.OptValue(c.options.Workspace))
}

func (c *Client) listSessions(ctx context.Context, cwd acp.Opt[string]) ([]harness.SessionInfo, error) {
	var result []harness.SessionInfo
	var cursor acp.Opt[string]
	seen := make(map[string]bool)
	for page := 0; page < 100; page++ {
		response, err := c.conn.ListSessions(ctx, &acp.ListSessionsRequest{Cwd: cwd, Cursor: cursor})
		if err != nil {
			return nil, mapError(err)
		}
		for _, info := range response.Sessions {
			result = append(result, sessionInfo(info))
		}
		next, ok := response.NextCursor.Get()
		if !ok || next == "" {
			return result, nil
		}
		if seen[next] {
			return nil, errors.New("agent returned a repeated session-list cursor")
		}
		seen[next], cursor = true, acp.OptValue(next)
	}
	return nil, errors.New("agent session list exceeds 100 pages")
}

func (c *Client) DeleteSession(ctx context.Context, id string) error {
	if !c.Peer().Capabilities.Delete {
		return harness.ErrUnsupported
	}
	_, err := c.conn.DeleteSession(ctx, &acp.DeleteSessionRequest{SessionID: acp.SessionID(id)})
	if err == nil {
		c.mu.Lock()
		delete(c.sessions, id)
		c.mu.Unlock()
		c.terminals.CloseSession(id)
		c.files.RemoveSession(id)
	}
	return mapError(err)
}

func (c *Client) CloseSession(ctx context.Context, id string) error {
	if !c.Peer().Capabilities.Close {
		return harness.ErrUnsupported
	}
	session, err := c.session(id)
	if err != nil {
		return err
	}
	_, err = session.Close(ctx, nil)
	if err == nil {
		c.mu.Lock()
		delete(c.sessions, id)
		c.mu.Unlock()
		c.terminals.CloseSession(id)
		c.files.RemoveSession(id)
	}
	return mapError(err)
}

func (c *Client) Prompt(ctx context.Context, id string, prompt harness.Prompt) (string, error) {
	session, err := c.session(id)
	if err != nil {
		return "", err
	}
	blocks := []acp.ContentBlock{&acp.TextContent{Text: prompt.Text}}
	for _, attachment := range prompt.Attachments {
		if attachment.Image {
			blocks = append(blocks, &acp.ImageContent{Data: base64.StdEncoding.EncodeToString(attachment.Data), MimeType: attachment.MIME})
		} else {
			blocks = append(blocks, &acp.ResourceLink{Name: attachment.Name, URI: attachment.URI, MimeType: acp.OptValue(attachment.MIME)})
		}
	}
	response, err := session.Prompt(ctx, &acp.PromptParams{Prompt: blocks})
	if err != nil {
		return "", mapError(err)
	}
	return string(response.StopReason), nil
}

func (c *Client) Cancel(ctx context.Context, id string) error {
	session, err := c.session(id)
	if err != nil {
		return err
	}
	return session.Cancel(ctx, nil)
}

func (c *Client) SetOption(ctx context.Context, id string, option harness.ConfigOption, newValue any) ([]harness.ConfigOption, error) {
	session, err := c.session(id)
	if err != nil {
		return nil, err
	}
	if option.Legacy {
		v, ok := newValue.(string)
		if !ok {
			return nil, errors.New("mode value must be a string")
		}
		_, err := session.SetMode(ctx, &acp.SetModeParams{ModeID: acp.SessionModeID(v)})
		if err != nil {
			return nil, err
		}
		option.Value = v
		return []harness.ConfigOption{option}, nil
	}
	params := &acp.SetConfigOptionParams{ConfigID: acp.SessionConfigID(option.ID)}
	switch v := newValue.(type) {
	case string:
		params.Value = &acp.SetSessionConfigOptionRequestValueID{Value: acp.SessionConfigValueID(v)}
	case bool:
		params.Value = &acp.SetSessionConfigOptionRequestBoolean{Value: v}
	default:
		return nil, errors.New("unsupported configuration value")
	}
	response, err := session.SetConfigOption(ctx, params)
	if err != nil {
		return nil, mapError(err)
	}
	return configOptions(response.ConfigOptions)
}

func (c *Client) Authenticate(ctx context.Context, id string) error {
	_, err := c.conn.Authenticate(ctx, &acp.AuthenticateRequest{MethodID: acp.AuthMethodID(id)})
	return mapError(err)
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.conn.Close()
		c.terminals.Close()
		c.files.Close()
	})
	return c.closeErr
}

func mapError(err error) error {
	if errors.Is(err, acp.ErrAuthRequired) {
		return fmt.Errorf("%w: %v", harness.ErrAuthRequired, err)
	}
	return err
}
