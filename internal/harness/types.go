// Package harness defines the protocol-independent boundary between the
// application and coding agents. ACP types must not cross this boundary.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var (
	ErrUnsupported  = errors.New("operation is not supported by this agent")
	ErrAuthRequired = errors.New("agent authentication is required")
)

type Capabilities struct {
	Load, Resume, List, Close, Delete                 bool
	Images, EmbeddedContext, HTTPMCP, AdditionalRoots bool
}

type Peer struct {
	Name, Version string
	Capabilities  Capabilities
	Auth          []AuthMethod
}

type AuthMethod struct {
	ID, Name, Description string
	Terminal              bool
	Args                  []string
	Env                   map[string]string
}

type Launch struct {
	Command string
	Args    []string
	Env     map[string]string
}

type Options struct {
	Workspace       string
	AdditionalRoots []string
	Launch          Launch
	// DefaultModes lists verified harness mode IDs, most permissive first.
	// Only advertised choices are selected, on session creation and load/resume.
	DefaultModes []string
	// MCP is ACP's standard session MCP-server configuration. Keeping it opaque
	// here avoids making the domain depend on an individual Go ACP SDK.
	MCP json.RawMessage
	// DecodeUsageLimits optionally extracts subscription usage windows from
	// agent-specific metadata. Adapters pass the raw metadata object when one is
	// present. Implementations must ignore unknown keys and malformed values.
	DecodeUsageLimits func(json.RawMessage) []UsageLimit
	Diagnostics       io.Writer
}

type SessionInfo struct {
	ID              string   `json:"sessionId"`
	Workspace       string   `json:"cwd"`
	Title           string   `json:"title"`
	UpdatedAt       string   `json:"updatedAt"`
	AdditionalRoots []string `json:"additionalDirectories,omitempty"`
}

// SessionInfoPatch preserves partial metadata updates. Nil means unchanged;
// a pointer to an empty string clears the field.
type SessionInfoPatch struct {
	Title, UpdatedAt *string
}

type Session struct {
	SessionInfo
	Options []ConfigOption
}

type ConfigOption struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Type        string   `json:"type"`
	Value       any      `json:"currentValue"`
	Choices     []Choice `json:"-"`
	// Legacy marks a modes-only agent. The adapter uses set_mode in this case.
	Legacy bool `json:"-"`
}

type Choice struct {
	Value, Name, Description, Group string
}

type Attachment struct {
	Path, Name, URI, MIME string
	Data                  []byte
	Image                 bool
}

type Prompt struct {
	Text        string
	Attachments []Attachment
}

type Command struct {
	Name, Description, Hint string
}

type MessageChunk struct {
	ID, Role, Text string
}

type Diff struct {
	Path, OldText, NewText string
}

// ToolPatch represents a partial update. Nil means unchanged, not empty.
type ToolPatch struct {
	ID                  string
	Title, Kind, Status *string
	Text                *string
	Diffs               *[]Diff
	Terminals           *[]string
	Locations           *[]string
	Input, Output       *json.RawMessage
}

type PlanEntry struct {
	Content, Status, Priority string
}

type Usage struct {
	Used, Size uint64
	Cost       *Cost
	// Limits contains optional subscription quota windows reported by the
	// harness. An empty slice means the harness has no limits to report.
	Limits []UsageLimit
}

type Cost struct {
	Amount   float64
	Currency string
}

// UsageLimit is one subscription quota window, such as a rolling five-hour
// session limit or a weekly cap. Limits are account-wide even though they
// arrive on a session's usage update.
type UsageLimit struct {
	// Kind is a stable harness-defined identifier such as "five_hour".
	Kind string
	// Label is a short display label such as "5h".
	Label string
	// Utilization is the fraction of the window consumed, in [0, 1].
	Utilization float64
	// ResetsAt is the window reset time, or the zero time when unknown.
	ResetsAt time.Time
	// Status is allowed, allowed_warning, or rejected.
	Status string
}

const (
	LimitAllowed  = "allowed"
	LimitWarning  = "allowed_warning"
	LimitRejected = "rejected"
)

type PermissionOption struct {
	ID, Name, Kind string
}

// Interaction is a request/response rendezvous, not a UI callback. The buffered
// response channel lets the UI answer without blocking its event loop.
type Interaction struct {
	ID, SessionID, Kind, Message, URL string
	Tool                              ToolPatch
	Options                           []PermissionOption
	Schema                            json.RawMessage
	Reply                             chan Decision
	Done                              <-chan struct{}
}

type Decision struct {
	Action   string // selected, accept, decline, or cancel
	OptionID string
	Content  map[string]any
}

type EventKind string

const (
	Connecting           EventKind = "connecting"
	Connected            EventKind = "connected"
	Disconnected         EventKind = "disconnected"
	SessionLoading       EventKind = "session_loading"
	SessionReady         EventKind = "session_ready"
	SessionsListed       EventKind = "sessions_listed"
	SessionDeleted       EventKind = "session_deleted"
	SessionClosed        EventKind = "session_closed"
	Message              EventKind = "message"
	Tool                 EventKind = "tool"
	Plan                 EventKind = "plan"
	Commands             EventKind = "commands"
	Config               EventKind = "config"
	Mode                 EventKind = "mode"
	Metadata             EventKind = "metadata"
	UsageChanged         EventKind = "usage"
	TurnStarted          EventKind = "turn_started"
	TurnFinished         EventKind = "turn_finished"
	InteractionRequested EventKind = "interaction_requested"
	InteractionFinished  EventKind = "interaction_finished"
	TerminalOutput       EventKind = "terminal_output"
	Notice               EventKind = "notice"
	Failure              EventKind = "failure"
)

// Event is a tagged application event. Only the payload for Kind is meaningful.
// Generation prevents a disconnected process from updating a new connection.
type Event struct {
	Kind             EventKind
	Operation        EventKind // originating lifecycle operation, including failures
	Generation       uint64
	SessionID        string
	At               time.Time
	Peer             Peer
	Session          Session
	Sessions         []SessionInfo
	Chunk            MessageChunk
	Tool             ToolPatch
	Plan             []PlanEntry
	Commands         []Command
	Options          []ConfigOption
	Metadata         SessionInfoPatch
	Usage            Usage
	Interaction      *Interaction
	ID, Text, Reason string
	Err              error
}

type Sink func(Event)

// Adapter owns a connection and its sessions. Calls may be concurrent; a
// session may have only one active Prompt. Close must release owned resources.
type Adapter interface {
	Peer() Peer
	NewSession(context.Context) (Session, error)
	LoadSession(context.Context, SessionInfo, bool) (Session, error)
	ListSessions(context.Context) ([]SessionInfo, error)
	DeleteSession(context.Context, string) error
	CloseSession(context.Context, string) error
	Prompt(context.Context, string, Prompt) (string, error)
	Cancel(context.Context, string) error
	SetOption(context.Context, string, ConfigOption, any) ([]ConfigOption, error)
	Authenticate(context.Context, string) error
	Close() error
}

type Factory func(context.Context, Options, Sink) (Adapter, error)
