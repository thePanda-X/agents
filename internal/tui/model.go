// Package tui implements the terminal interface. I/O is issued through commands;
// all presentation state is owned by this model's single Update loop.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/storage"
	"agents/internal/tui/overlays"
	"agents/internal/tui/theme"
	"agents/internal/tui/transcript"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Options struct {
	Context    context.Context
	Controller *app.Controller
	Registry   *harness.Registry
	Descriptor harness.Descriptor
	Agent      harness.Options
	Settings   storage.Settings
	Snapshot   storage.Snapshot
	Store      *storage.Store
	Restore    *harness.SessionInfo
	Demo       bool
}

type eventMsg harness.Event
type tickMsg time.Time
type filesMsg struct {
	files      []string
	err        error
	generation uint64
}
type attachedMsg struct {
	attachment harness.Attachment
	err        error
	buffer     *draftBuffer
	generation uint64
}
type editorMsg struct {
	text string
	err  error
}
type authMsg struct{ err error }
type saveMsg struct{ err error }
type urlMsg struct {
	interaction *harness.Interaction
	err         error
}

type Model struct {
	opts                                                   Options
	state                                                  *app.State
	input                                                  textarea.Model
	viewport                                               viewport.Model
	info                                                   viewport.Model
	renderer                                               *transcript.Renderer
	width, height                                          int
	focusedTranscript                                      bool
	selected                                               int
	browseLine                                             int
	browseVisited                                          bool
	positions                                              []int
	expanded                                               map[string]bool
	list                                                   *overlays.List
	overlay, infoTitle                                     string
	option                                                 harness.ConfigOption
	interaction                                            *harness.Interaction
	form                                                   *overlays.Form
	attachments                                            []harness.Attachment
	clipboardGeneration                                    uint64
	pastingImage                                           bool
	creatingSession                                        bool
	pendingFirstPrompt                                     *harness.Prompt
	pendingFirstDraft                                      string
	toolSpinnerFrame                                       int
	effortPending                                          bool
	pendingSetup                                           *sessionSetupRequest
	configPending                                          bool
	history                                                []string
	historyIndex                                           int
	draftBeforeHistory                                     string
	dirty, unseen, submitting, sessionPending, demoStarted bool
	lastSavedDraft                                         string
	lastSave                                               time.Time
	quitAt                                                 time.Time
	saveRevision                                           uint64
	urlReviewed, urlOpening                                bool
	searchQuery                                            string
	searchEntry, searchOccurrence, searchOccurrences       int
	browsePrefix                                           string
	browsePrefixAt                                         time.Time
	clearedDraft                                           string
	infoHunks                                              []int
	infoPaths                                              []string
	infoContent                                            string
	recentActions                                          []string
	filePickerGeneration                                   uint64
	filePreviewPath                                        string
	skillsGeneration                                       uint64
	displayedSessionID                                     string
	commandAgentOnly                                       bool
	drafts                                                 map[draftKey]*draftBuffer
	hasWorked                                              bool
	overlayStack                                           []overlayFrame
	noticeText, errorText                                  string
	noticeAt, errorAt                                      time.Time
}

func New(options Options) *Model {
	input := textarea.New()
	input.Placeholder, input.Prompt = "Ask anything…", "› "
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.CharLimit = 128 * 1024
	// Let Bubbles measure soft-wrapped visual rows. MaxHeight also limits
	// logical newline insertion, so leave it unlimited and cap the viewport
	// separately in resize (long drafts must remain editable, not truncated).
	input.DynamicHeight, input.MinHeight, input.MaxHeight, input.MaxWidth = true, 1, 0, 0
	input.SetWidth(76)
	input.SetHeight(1)
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "shift+enter", "ctrl+j"))
	style := input.Styles()
	style.Focused.Base, style.Blurred.Base = theme.Body.Background(theme.Surface), theme.Body.Background(theme.Surface)
	style.Focused.Text, style.Blurred.Text = theme.Body, theme.Body
	style.Focused.Prompt, style.Blurred.Prompt = theme.Accent, theme.Quiet
	style.Focused.Placeholder, style.Blurred.Placeholder = theme.Quiet, theme.Quiet
	style.Focused.CursorLine, style.Blurred.CursorLine = theme.Body, theme.Body
	style.Cursor.Color = theme.Pink
	input.SetStyles(style)
	m := &Model{opts: options, state: app.NewState(), input: input, viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)), info: viewport.New(), renderer: transcript.New(), expanded: make(map[string]bool), width: 84, height: 30, dirty: true}
	m.saveRevision = options.Snapshot.Revision
	workspace := options.Snapshot.Workspaces[storage.Key(options.Agent.Workspace, options.Descriptor.ID)]
	m.hasWorked = workspace.Session.ID != ""
	if options.Restore == nil {
		m.input.SetValue(workspace.Drafts[""])
		m.lastSavedDraft = m.input.Value()
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), m.connect(m.opts.Restore), m.awaitEvent(), tick())
}

func command(fn func()) tea.Cmd { return func() tea.Msg { fn(); return nil } }

func tick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(at time.Time) tea.Msg { return tickMsg(at) })
}

func (m *Model) awaitEvent() tea.Cmd {
	ctx, events := m.opts.Context, m.opts.Controller.Events
	return func() tea.Msg {
		select {
		case e := <-events:
			return eventMsg(e)
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *Model) connect(restore *harness.SessionInfo) tea.Cmd {
	controller, descriptor, options := m.opts.Controller, m.opts.Descriptor, m.opts.Agent
	m.sessionPending = restore != nil
	m.creatingSession, m.pendingFirstPrompt = false, nil
	m.effortPending = false
	m.pendingSetup = nil
	m.configPending = false
	return command(func() { controller.Connect(descriptor, options, restore) })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.dirty = true
	case eventMsg:
		e := harness.Event(msg)
		oldID := m.state.ActiveID
		oldKey := m.draftKey()
		if e.Kind == harness.SessionClosed && e.Generation == m.state.Generation && e.SessionID == oldID {
			m.stashDraft()
		}
		if e.Kind == harness.SessionReady && e.Generation == m.state.Generation && oldID != e.Session.ID {
			m.stashDraft()
			if previous := m.state.Active(); previous != nil {
				previous.Draft, previous.DraftLoaded = m.input.Value(), true
			} else if !m.creatingSession {
				m.rememberLocalDraft()
			}
		}
		if m.state.Apply(e) {
			m.dirty = true
			if !m.viewport.AtBottom() && (e.Kind == harness.Message || e.Kind == harness.Tool || e.Kind == harness.TerminalOutput) {
				m.unseen = true
			}
			switch e.Kind {
			case harness.Connecting:
				m.effortPending = false
				m.configPending = false
			case harness.Config:
				if e.SessionID == m.state.ActiveID {
					m.configPending = false
					if m.effortPending {
						m.state.Notice = "Thinking effort updated"
					} else {
						m.state.Notice = "Session options updated"
					}
					m.effortPending = false
					if m.overlay == "options" {
						m.populateOptions()
					}
				}
			case harness.Connected:
				if m.opts.Demo && !m.demoStarted && !m.sessionPending {
					m.demoStarted = true
					m.input.SetValue("Show me the interface")
					cmds = append(cmds, m.send())
				}
			case harness.SessionReady:
				m.effortPending = false
				m.configPending = false
				setup := m.pendingSetup
				m.pendingSetup = nil
				creating := m.creatingSession
				prompt, draft := m.pendingFirstPrompt, m.pendingFirstDraft
				m.creatingSession, m.pendingFirstPrompt = false, nil
				m.sessionPending = false
				if creating {
					if buffer := m.drafts[oldKey]; buffer != nil {
						m.drafts[m.draftKey()] = buffer
						delete(m.drafts, oldKey)
					}
				}
				if oldID != m.state.ActiveID || e.Session.ID != m.displayedSessionID {
					if !creating {
						m.clipboardGeneration++
						m.pastingImage = false
						m.restoreAttachments()
					}
					m.history = nil
					if c := m.state.Active(); c != nil {
						for _, entry := range c.Entries {
							if entry.Role == "user" {
								m.history = append(m.history, entry.Text)
							}
						}
					}
					m.historyIndex = len(m.history)
					m.selected = 0
					m.browseVisited = false
					m.searchQuery, m.browsePrefix = "", ""
					m.viewport.GotoBottom()
					if !creating {
						m.restoreDraft()
					}
				}
				m.displayedSessionID = e.Session.ID
				if creating {
					m.clearLocalDraft()
					if prompt != nil {
						cmds = append(cmds, m.submitPrompt(*prompt, draft))
					}
				}
				if setup != nil {
					cmds = append(cmds, m.finishSessionSetup(*setup))
				}
				if m.opts.Demo && !m.demoStarted {
					m.demoStarted = true
					m.input.SetValue("Show me the interface")
					cmds = append(cmds, m.send())
				}
			case harness.Failure, harness.Disconnected:
				if (m.submitting && e.Operation == "") || e.Kind == harness.Disconnected {
					m.finishSubmission(true)
				}
				if m.list != nil && m.list.Loading && (e.Kind == harness.Disconnected || e.Operation == "" || m.overlay == "sessions" && (e.Operation == harness.SessionsListed || e.Operation == harness.SessionDeleted)) {
					m.list.Loading = false
					m.list.Error = "Could not load " + m.list.Title + "."
					m.list.Description = "Enter retries · Esc closes. Use /diagnostics for details."
					m.list.Hint = "Enter retry · Esc close"
				}
				if e.Operation == "" || e.Operation == harness.Config || e.Kind == harness.Disconnected {
					m.configPending, m.effortPending = false, false
				}
				if e.Operation == "" || e.Operation == harness.SessionReady || e.Kind == harness.Disconnected {
					m.pendingSetup = nil
					m.sessionPending, m.submitting = false, false
					m.creatingSession, m.pendingFirstPrompt = false, nil
				}
			case harness.TurnStarted:
				m.submitting = false
			case harness.TurnFinished:
				if e.SessionID == m.state.ActiveID {
					m.hasWorked = true
					m.finishSubmission(e.Err != nil || e.Reason == "cancelled")
					if e.Err == nil && e.Reason != "" && e.Reason != "end_turn" && (e.Reason != "cancelled" || m.currentDraft().Failed == nil) {
						m.state.Notice = "Turn ended · " + e.Reason
					}
				}
			case harness.Commands:
				if m.overlay == "commands" && e.SessionID == m.state.ActiveID {
					m.list.Items = m.slashItems(m.commandAgentOnly)
				}
			case harness.SessionsListed:
				if m.overlay == "sessions" {
					m.populateSessions()
				}
			case harness.SessionDeleted:
				m.forgetSession(e.SessionID)
				for key, buffer := range m.drafts {
					if key.Harness == m.opts.Descriptor.ID && key.Session == e.SessionID {
						buffer.Generation++
						delete(m.drafts, key)
					}
				}
				if oldID == e.SessionID {
					m.input.Reset()
					m.attachments = nil
					m.restoreLocalBuffer()
				}
				if m.overlay == "sessions" {
					m.populateSessions()
				}
			case harness.SessionClosed:
				if e.SessionID == oldID {
					m.stashClosedDraft(oldKey)
				}
			}
		}
		m.syncInteraction()
		cmds = append(cmds, m.awaitEvent())
	case tickMsg:
		frame := int(time.Time(msg).UnixMilli() / 120)
		if frame != m.toolSpinnerFrame && !m.opts.Settings.ReducedMotion {
			m.toolSpinnerFrame = frame
			if c := m.state.Active(); c != nil {
				for _, entry := range c.Entries {
					if entry.Tool != nil && (entry.Tool.Status == "pending" || entry.Tool.Status == "in_progress") {
						m.dirty = true
						break
					}
				}
			}
		}
		if m.dirty {
			m.refresh()
			m.dirty = false
		}
		m.syncInteraction()
		if m.input.Value() != m.lastSavedDraft && time.Since(m.lastSave) > time.Second {
			cmds = append(cmds, m.save())
		}
		cmds = append(cmds, tick())
	case filesMsg:
		if m.overlay != "files" || msg.generation != m.filePickerGeneration {
			break
		}
		if msg.err != nil {
			m.list.Loading = false
			m.state.LastError = msg.err
			m.list.Error = "Could not load workspace files."
			m.list.Hint = "Enter retry · Esc close"
			m.list.Description = "Enter retries · /diagnostics shows details."
		} else if m.overlay == "files" {
			m.list.Loading = false
			m.list.Description = "Tab marks files · Enter attaches marked files (or current file)."
			m.list.Items = nil
			for _, path := range msg.files {
				m.list.Items = append(m.list.Items, overlays.Item{ID: path, Label: path, Value: path})
			}
		}
	case filePreviewMsg:
		if m.overlay == "files" && msg.generation == m.filePickerGeneration {
			for i := range m.list.Items {
				if m.list.Items[i].ID == msg.path {
					m.list.Items[i].Preview = msg.text
					break
				}
			}
		}
	case skillsMsg:
		m.populateSkills(msg)
	case attachedMsg:
		m.receiveAttachment(msg)
	case clipboardImageMsg:
		cmds = append(cmds, m.receiveClipboardImage(msg))
	case editorMsg:
		if msg.err != nil {
			m.state.LastError = msg.err
		} else {
			m.input.SetValue(msg.text)
		}
	case authMsg:
		if msg.err != nil {
			m.state.LastError = msg.err
		} else {
			cmds = append(cmds, m.reconnect())
		}
	case handoffMsg:
		if msg.err != nil {
			m.state.LastError = msg.err
		}
	case saveMsg:
		if msg.err != nil {
			m.state.LastError = fmt.Errorf("save client state: %w", msg.err)
		}
	case urlMsg:
		m.urlOpening = false
		if msg.err != nil {
			m.state.LastError = msg.err
			m.answer(msg.interaction, harness.Decision{Action: "cancel"})
		} else {
			m.answer(msg.interaction, harness.Decision{Action: "accept"})
		}
	case tea.KeyPressMsg:
		cmds = append(cmds, m.handleKey(msg))
	case tea.MouseWheelMsg:
		if m.overlay == "info" {
			var cmd tea.Cmd
			m.info, cmd = m.info.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.list == nil && m.interaction == nil {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
			if m.viewport.AtBottom() {
				m.unseen = false
			}
		}
	default:
		if m.interaction != nil && m.form != nil {
			cmds = append(cmds, m.form.Update(msg))
		} else if m.list != nil {
			cmds = append(cmds, m.list.Update(msg))
		} else if !m.focusedTranscript {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	m.resize()
	m.trackFeedback()
	if m.overlay == "files" {
		cmds = append(cmds, m.previewFile())
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) resize() {
	width := max(8, m.width-4)
	chrome := 8
	if m.opts.Settings.Compact {
		chrome = 6
	}
	m.input.SetWidth(max(4, width-4))
	m.input.SetHeight(min(m.input.Height(), max(1, min(8, m.height/3, m.height-chrome-1-boolInt(len(m.attachments) > 0)))))
	newHeight := max(1, m.height-m.input.Height()-chrome-boolInt(len(m.attachments) > 0))
	if m.viewport.Width() != width || m.viewport.Height() != newHeight {
		m.dirty = true
	}
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(newHeight)
	m.sizeInfo()
	if m.overlay == "info" && m.infoContent != "" {
		wrapped := lipgloss.Wrap(m.infoContent, m.info.Width(), " ")
		m.info.SetContent(wrapped)
		m.infoHunks = nil
		for index, line := range strings.Split(wrapped, "\n") {
			if strings.HasPrefix(theme.Clean(line), "@@") {
				m.infoHunks = append(m.infoHunks, index)
			}
		}
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (m *Model) refresh() {
	follow := m.viewport.AtBottom()
	text, positions := m.renderer.Render(m.state.Active(), transcript.Options{Width: m.viewport.Width(), Selected: m.selected, Focused: m.focusedTranscript, Thoughts: m.opts.Settings.ShowThoughts, Compact: m.opts.Settings.Compact, Search: m.searchQuery, SearchOccurrence: m.searchOccurrence, Expanded: m.expanded, Terminals: m.state.TerminalOutputs, SpinnerFrame: m.toolSpinnerFrame, ReducedMotion: m.opts.Settings.ReducedMotion})
	m.positions = positions
	m.viewport.SetContent(text)
	if follow && !m.focusedTranscript {
		m.viewport.GotoBottom()
		m.unseen = false
	}
}

func (m *Model) send() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if cmd, handled := m.handleSlashCommand(text); handled {
		return cmd
	}
	return m.sendPrompt(text)
}

func (m *Model) sendPrompt(text string) tea.Cmd {
	if m.configPending {
		m.state.Notice = "Applying session options · draft preserved · send when configuration finishes"
		return nil
	}
	if m.pastingImage {
		m.state.Notice = "Reading clipboard · draft preserved · send when the image is attached"
		return nil
	}
	if m.currentDraft().Pending > 0 {
		m.state.Notice = "Reading attachments · draft preserved · send when files are ready"
		return nil
	}
	for _, attachment := range m.attachments {
		if attachment.Image && !m.state.Peer.Capabilities.Images {
			m.state.LastError = fmt.Errorf("this agent does not support image prompts; remove the image with /attachments")
			return nil
		}
	}
	c := m.state.Active()
	if c != nil && (c.Busy || c.Loading) || m.submitting || m.sessionPending || m.state.Connection != "ready" {
		if c != nil && c.Busy {
			m.state.Notice = "Turn running · draft preserved · Ctrl+C to stop"
		} else {
			m.state.Notice = "Session not ready · draft preserved"
		}
		return nil
	}
	if text == "" && len(m.attachments) == 0 {
		return nil
	}
	prompt := harness.Prompt{Text: text, Attachments: append([]harness.Attachment(nil), m.attachments...)}
	if c == nil {
		m.pendingFirstPrompt, m.pendingFirstDraft = &prompt, m.input.Value()
		m.sessionPending, m.creatingSession = true, true
		m.state.Notice = "Creating conversation · draft preserved · Ctrl+C to cancel the first send"
		return command(m.opts.Controller.NewSession)
	}
	return m.submitPrompt(prompt, m.input.Value())
}

func (m *Model) submitPrompt(prompt harness.Prompt, draft string) tea.Cmd {
	c := m.state.Active()
	id, controller := c.Info.ID, m.opts.Controller
	m.submitting = true
	buffer := m.currentDraft()
	buffer.Submission = &submissionSnapshot{Draft: draft, Prompt: harness.Prompt{Text: prompt.Text, Attachments: append([]harness.Attachment(nil), prompt.Attachments...)}}
	m.history = append(m.history, prompt.Text)
	m.historyIndex = len(m.history)
	if m.input.Value() == draft {
		m.input.Reset()
	}
	c.Draft, c.DraftLoaded = m.input.Value(), true
	m.attachments = m.attachments[min(len(prompt.Attachments), len(m.attachments)):]
	m.stashDraft()
	m.focusedTranscript = false
	m.input.Focus()
	m.viewport.GotoBottom()
	return command(func() { controller.Prompt(id, prompt) })
}

func (m *Model) restoreDraft() {
	c := m.state.Active()
	if c == nil {
		return
	}
	if !c.DraftLoaded {
		persisted := m.opts.Snapshot.Workspaces[storage.Key(c.Info.Workspace, m.opts.Descriptor.ID)]
		c.Draft = persisted.Drafts[c.Info.ID]
		c.DraftLoaded = true
	}
	m.input.SetValue(c.Draft)
	m.lastSavedDraft = c.Draft
}

func (m *Model) Snapshot() (storage.Settings, storage.Snapshot) {
	m.stashDraft()
	if c := m.state.Active(); c != nil {
		c.Draft = m.input.Value()
		c.DraftLoaded = true
	}
	m.saveRevision++
	snapshot := storage.Snapshot{Version: 1, Revision: m.saveRevision, Workspaces: make(map[string]storage.WorkspaceState)}
	for key, workspace := range m.opts.Snapshot.Workspaces {
		snapshot.Workspaces[key] = workspace
	}
	if !m.opts.Demo {
		for _, c := range m.state.Conversations {
			if c.Info.Workspace == "" {
				continue
			}
			key := storage.Key(c.Info.Workspace, m.opts.Descriptor.ID)
			workspace := snapshot.Workspaces[key]
			drafts := make(map[string]string)
			for id, draft := range workspace.Drafts {
				drafts[id] = draft
			}
			if c.Draft != "" {
				drafts[c.Info.ID] = c.Draft
			} else {
				delete(drafts, c.Info.ID)
			}
			workspace.Drafts = drafts
			if c.Info.ID == m.state.ActiveID {
				workspace.Session = c.Info
			}
			snapshot.Workspaces[key] = workspace
		}
		if m.state.Active() == nil {
			key := storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)
			workspace := snapshot.Workspaces[key]
			drafts := make(map[string]string)
			for id, draft := range workspace.Drafts {
				drafts[id] = draft
			}
			if m.input.Value() == "" {
				delete(drafts, "")
			} else {
				drafts[""] = m.input.Value()
			}
			workspace.Drafts = drafts
			snapshot.Workspaces[key] = workspace
		}
	}
	return m.opts.Settings, snapshot
}

func (m *Model) save() tea.Cmd {
	if m.opts.Store == nil {
		return nil
	}
	settings, snapshot := m.Snapshot()
	m.opts.Snapshot = snapshot
	m.lastSavedDraft, m.lastSave = m.input.Value(), time.Now()
	store := m.opts.Store
	return func() tea.Msg { return saveMsg{err: store.Save(settings, snapshot)} }
}

func (m *Model) forgetSession(id string) {
	for key, workspace := range m.opts.Snapshot.Workspaces {
		drafts := make(map[string]string)
		for draftID, draft := range workspace.Drafts {
			if draftID != id {
				drafts[draftID] = draft
			}
		}
		workspace.Drafts = drafts
		if workspace.Session.ID == id {
			workspace.Session = harness.SessionInfo{}
		}
		m.opts.Snapshot.Workspaces[key] = workspace
	}
}

func (m *Model) reconnect() tea.Cmd {
	m.stashDraft()
	m.finishSubmission(true)
	var restore *harness.SessionInfo
	if c := m.state.Active(); c != nil {
		c.Draft = m.input.Value()
		c.DraftLoaded = true
		info := c.Info
		restore = &info
	}
	return m.connect(restore)
}

func (m *Model) newSession() tea.Cmd {
	if m.sessionPending {
		return nil
	}
	if c := m.state.Active(); c != nil {
		if c.Busy {
			m.state.Notice = "Cancel the active turn before opening another session."
			return nil
		}
		c.Draft = m.input.Value()
		c.DraftLoaded = true
		m.stashDraft()
	} else {
		m.state.Notice = "New conversation · send a prompt to create the session"
		return nil
	}
	m.state.ActiveID, m.displayedSessionID = "", ""
	m.input.Reset()
	m.attachments, m.history, m.positions = nil, nil, nil
	m.clipboardGeneration++
	m.pastingImage = false
	m.restoreLocalBuffer()
	m.historyIndex, m.selected, m.browseLine = 0, 0, 0
	m.browseVisited = false
	m.searchQuery, m.browsePrefix = "", ""
	m.dirty = true
	m.state.Notice = "New conversation · send a prompt to create the session"
	return m.setTranscriptFocus(false)
}

func (m *Model) rememberLocalDraft() {
	key := storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)
	workspace := m.opts.Snapshot.Workspaces[key]
	drafts := make(map[string]string)
	for id, draft := range workspace.Drafts {
		drafts[id] = draft
	}
	if m.input.Value() == "" {
		delete(drafts, "")
	} else {
		drafts[""] = m.input.Value()
	}
	workspace.Drafts = drafts
	if m.opts.Snapshot.Workspaces == nil {
		m.opts.Snapshot.Workspaces = make(map[string]storage.WorkspaceState)
	}
	m.opts.Snapshot.Workspaces[key] = workspace
}

func (m *Model) clearLocalDraft() {
	if m.opts.Demo {
		return
	}
	key := storage.Key(m.opts.Agent.Workspace, m.opts.Descriptor.ID)
	workspace := m.opts.Snapshot.Workspaces[key]
	drafts := make(map[string]string)
	for id, draft := range workspace.Drafts {
		if id != "" {
			drafts[id] = draft
		}
	}
	workspace.Drafts = drafts
	if m.opts.Snapshot.Workspaces == nil {
		m.opts.Snapshot.Workspaces = make(map[string]storage.WorkspaceState)
	}
	m.opts.Snapshot.Workspaces[key] = workspace
}
