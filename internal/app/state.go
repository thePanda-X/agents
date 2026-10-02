// Package app owns conversation state. It knows neither Bubble Tea nor ACP.
package app

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"agents/internal/harness"
)

type Entry struct {
	ID, Kind, Role, Text string
	At                   time.Time
	Revision             uint64
	Tool                 *ToolState
}

type ToolState struct {
	ID, Title, Kind, Status, Text string
	Diffs                         []harness.Diff
	Terminals, Locations          []string
	Input, Output                 json.RawMessage
}

type Conversation struct {
	Info                      harness.SessionInfo
	Entries                   []*Entry
	Options                   []harness.ConfigOption
	Commands                  []harness.Command
	Plan                      []harness.PlanEntry
	Usage                     *harness.Usage
	Draft                     string
	DraftLoaded               bool
	Busy, Cancelling, Loading bool
	StartedAt                 time.Time
	StopReason                string
	messageIndex              map[string]*Entry
	toolIndex                 map[string]*Entry
	pendingUser               *Entry
	pendingEcho               string
}

type State struct {
	Generation      uint64
	Connection      string
	Peer            harness.Peer
	ActiveID        string
	Conversations   map[string]*Conversation
	Sessions        []harness.SessionInfo
	Interactions    []*harness.Interaction
	TerminalOutputs map[string]string
	Notice          string
	LastError       error
}

func NewState() *State {
	return &State{Connection: "starting", Conversations: make(map[string]*Conversation), TerminalOutputs: make(map[string]string)}
}

func (s *State) Active() *Conversation { return s.Conversations[s.ActiveID] }

func (s *State) conversation(id string) *Conversation {
	c := s.Conversations[id]
	if c == nil {
		c = &Conversation{Info: harness.SessionInfo{ID: id}, messageIndex: make(map[string]*Entry), toolIndex: make(map[string]*Entry)}
		s.Conversations[id] = c
	}
	return c
}

// Apply is the only writer of agent-derived presentation state. Events are
// applied in arrival order; stale generations are ignored.
func (s *State) Apply(e harness.Event) bool {
	if e.Kind == harness.Connecting {
		if e.Generation < s.Generation {
			return false
		}
		s.Generation, s.Connection, s.LastError = e.Generation, "connecting", nil
		s.Interactions = nil
		for _, c := range s.Conversations {
			c.Busy, c.Cancelling, c.Loading = false, false, false
			c.pendingUser, c.pendingEcho = nil, ""
		}
		return true
	}
	if e.Generation != s.Generation {
		return false
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	switch e.Kind {
	case harness.Connected:
		s.Peer, s.Connection = e.Peer, "ready"
	case harness.Disconnected:
		s.Connection = "disconnected"
		s.Interactions = nil
		for _, c := range s.Conversations {
			c.Busy, c.Cancelling, c.Loading = false, false, false
		}
		if e.Err != nil {
			s.LastError = e.Err
		}
	case harness.Failure:
		s.LastError = e.Err
		if e.SessionID != "" {
			s.conversation(e.SessionID).Loading = false
		}
	case harness.Notice:
		s.Notice = e.Text
	case harness.SessionsListed:
		s.Sessions = e.Sessions
	case harness.SessionLoading:
		c := s.conversation(e.SessionID)
		c.Entries = nil
		c.messageIndex, c.toolIndex = make(map[string]*Entry), make(map[string]*Entry)
		c.pendingUser, c.pendingEcho = nil, ""
		c.Loading = true
		c.Busy, c.Cancelling = false, false
		// Keep the displayed conversation (and its draft) selected until the
		// lifecycle request succeeds. Replay events are staged by session ID.
	case harness.SessionReady:
		c := s.conversation(e.Session.ID)
		if e.Session.Title == "" {
			e.Session.Title = c.Info.Title
		}
		c.Info, c.Options, c.Loading = e.Session.SessionInfo, e.Session.Options, false
		s.ActiveID = e.Session.ID
		s.LastError = nil
	case harness.SessionDeleted:
		delete(s.Conversations, e.SessionID)
		s.Sessions = slices.DeleteFunc(s.Sessions, func(info harness.SessionInfo) bool { return info.ID == e.SessionID })
		if s.ActiveID == e.SessionID {
			s.ActiveID = ""
		}
	case harness.SessionClosed:
		if s.ActiveID == e.SessionID {
			s.ActiveID = ""
		}
	case harness.InteractionRequested:
		s.Interactions = append(s.Interactions, e.Interaction)
		if e.SessionID != "" && e.Interaction.Kind == "permission" {
			s.applyTool(s.conversation(e.SessionID), e.Interaction.Tool, e.At)
		}
	case harness.InteractionFinished:
		s.Interactions = slices.DeleteFunc(s.Interactions, func(i *harness.Interaction) bool { return i.ID == e.ID })
	case harness.TerminalOutput:
		s.TerminalOutputs[e.ID] = e.Text
	default:
		if e.SessionID == "" {
			return false
		}
		c := s.conversation(e.SessionID)
		switch e.Kind {
		case harness.TurnStarted:
			s.Notice = ""
			c.Busy, c.Cancelling, c.StartedAt, c.StopReason = true, false, e.At, ""
			s.LastError = nil
			// The agent may echo this prompt. The reducer reconciles its first
			// user chunk with this optimistic entry instead of duplicating it.
			entry := &Entry{Kind: "message", Role: "user", Text: e.Text, At: e.At, ID: "local", Revision: 1}
			c.Entries = append(c.Entries, entry)
			c.pendingUser, c.pendingEcho = entry, ""
		case harness.TurnFinished:
			c.pendingUser, c.pendingEcho = nil, ""
			c.Busy, c.Cancelling, c.StopReason = false, false, e.Reason
			if e.Err != nil {
				s.LastError = e.Err
			}
			for _, entry := range c.Entries {
				if entry.Tool != nil && (entry.Tool.Status == "pending" || entry.Tool.Status == "in_progress") && e.Reason == "cancelled" {
					entry.Tool.Status = "cancelled"
					entry.Revision++
				}
			}
		case harness.Message:
			s.applyMessage(c, e.Chunk, e.At)
		case harness.Tool:
			s.applyTool(c, e.Tool, e.At)
		case harness.Config:
			c.Options = e.Options
		case harness.Mode:
			for i := range c.Options {
				if c.Options[i].Legacy {
					c.Options[i].Value = e.ID
				}
			}
		case harness.Commands:
			c.Commands = e.Commands
		case harness.Plan:
			c.Plan = e.Plan
		case harness.UsageChanged:
			c.Usage = &e.Usage
		case harness.Metadata:
			if e.Text != "" {
				c.Info.Title = e.Text
			}
		}
	}
	return true
}

func (s *State) applyMessage(c *Conversation, chunk harness.MessageChunk, at time.Time) {
	if chunk.Role == "user" && c.pendingUser != nil && !c.Loading {
		entry := c.pendingUser
		c.pendingEcho += chunk.Text
		entry.ID = chunk.ID
		if chunk.ID != "" {
			c.messageIndex[chunk.Role+":"+chunk.ID] = entry
		}
		if !strings.HasPrefix(entry.Text, c.pendingEcho) || len(c.pendingEcho) >= len(entry.Text) {
			entry.Text = c.pendingEcho
			entry.Revision++
			c.pendingUser, c.pendingEcho = nil, ""
		}
		return
	}
	key := chunk.Role + ":" + chunk.ID
	entry := c.messageIndex[key]
	if chunk.ID == "" {
		entry = nil
		if n := len(c.Entries); n > 0 {
			last := c.Entries[n-1]
			if last.Kind == "message" && last.Role == chunk.Role && last.ID != "local" {
				entry = last
			}
		}
	}
	if entry == nil {
		// Only reconcile a live optimistic user echo, never replayed history.
		if chunk.Role == "user" && !c.Loading && len(c.Entries) > 0 {
			last := c.Entries[len(c.Entries)-1]
			if last.ID == "local" && last.Role == "user" && last.Text == chunk.Text {
				last.ID = chunk.ID
				if chunk.ID != "" {
					c.messageIndex[key] = last
				}
				return
			}
		}
		entry = &Entry{ID: chunk.ID, Kind: "message", Role: chunk.Role, At: at}
		c.Entries = append(c.Entries, entry)
		if chunk.ID != "" {
			c.messageIndex[key] = entry
		}
	}
	entry.Text += chunk.Text
	entry.Revision++
}

func (s *State) applyTool(c *Conversation, patch harness.ToolPatch, at time.Time) {
	entry := c.toolIndex[patch.ID]
	if entry == nil {
		entry = &Entry{ID: patch.ID, Kind: "tool", At: at, Tool: &ToolState{ID: patch.ID, Title: "Tool", Status: "pending"}}
		c.toolIndex[patch.ID] = entry
		c.Entries = append(c.Entries, entry)
	}
	t := entry.Tool
	if patch.Title != nil {
		t.Title = *patch.Title
	}
	if patch.Kind != nil {
		t.Kind = *patch.Kind
	}
	if patch.Status != nil {
		t.Status = *patch.Status
	}
	if patch.Text != nil {
		t.Text = *patch.Text
	}
	if patch.Diffs != nil {
		t.Diffs = *patch.Diffs
	}
	if patch.Terminals != nil {
		t.Terminals = *patch.Terminals
	}
	if patch.Locations != nil {
		t.Locations = *patch.Locations
	}
	if patch.Input != nil {
		t.Input = *patch.Input
	}
	if patch.Output != nil {
		t.Output = *patch.Output
	}
	entry.Revision++
}
