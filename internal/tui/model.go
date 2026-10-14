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
	transcriptContent                                      string
	textSelection                                          *textSelection
	expanded                                               map[string]bool
	list                                                   *overlays.List
	overlay, infoTitle                                     string
	option                                                 harness.ConfigOption
	interaction                                            *harness.Interaction
	form                                                   *overlays.Form
	attachments                                            []harness.Attachment
	clipboardGeneration                                    uint64
	pastingImage                                           bool
	pastingText                                            bool
	nextPasteID                                            uint64
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
	savePending                                            uint64
	saveFailures                                           int
	saveRetry                                              bool
	saveRetryAt                                            time.Time
	saveError                                              error
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
	branchWorkspace, branch                                string
	branchPending                                          bool
	branchChecked                                          time.Time
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
		m.setDraftText(workspace.Drafts[""])
		m.lastSavedDraft = m.input.Value()
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), m.connect(m.opts.Restore), m.awaitEvent(), m.refreshBranch(time.Now()), tick())
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

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if m.width != msg.Width || m.height != msg.Height {
			m.clearTextSelection()
		}
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
				previous.Draft, previous.DraftLoaded = m.draftText(), true
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
				m.pendingFirstDraft = ""
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
						m.pastingImage, m.pastingText = false, false
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
					m.pendingFirstDraft = ""
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
			case harness.Metadata:
				if m.overlay == "sessions" && !m.list.Loading {
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
		cmds = append(cmds, m.refreshBranch(time.Time(msg)))
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
		cmds = append(cmds, m.autosave(time.Time(msg)))
		cmds = append(cmds, tick())
	case branchMsg:
		m.branchPending = false
		if msg.workspace == m.activeWorkspace() {
			m.branchWorkspace, m.branch = msg.workspace, msg.branch
		}
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
	case clipboardTextMsg:
		m.receiveClipboardText(msg)
	case tea.PasteMsg:
		if m.interaction != nil && m.form != nil {
			cmds = append(cmds, m.form.Update(msg))
		} else if m.list != nil {
			cmds = append(cmds, m.list.Update(msg))
		} else if m.overlay == "" && !m.focusedTranscript {
			m.receiveTextPaste(msg.Content)
		}
	case editorMsg:
		if msg.err != nil {
			m.state.LastError = msg.err
		} else {
			m.setDraftText(msg.text)
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
		m.receiveSave(msg)
	case linkOpenedMsg:
		if msg.err != nil {
			m.state.LastError = fmt.Errorf("could not open link: %w", msg.err)
		} else {
			m.state.Notice = "Opened link in browser"
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
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		cmds = append(cmds, m.handleMouse(msg.(tea.MouseMsg)))
	case tea.MouseWheelMsg:
		if m.overlay == "info" {
			var cmd tea.Cmd
			m.info, cmd = m.info.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.list == nil && m.interaction == nil {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
			if m.textSelection != nil && m.textSelection.dragging {
				m.extendTextSelection(msg.Mouse())
			}
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
			before := m.input.Value()
			m.input, cmd = m.input.Update(msg)
			if m.input.Value() != before {
				m.clearTextSelection()
			}
			cmds = append(cmds, cmd)
		}
	}
	if m.textSelection != nil && (m.overlay != "" || m.interaction != nil) {
		m.textSelection.dragging = false
	}
	m.syncPastes()
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
	previousWidth := m.input.Width()
	m.input.SetWidth(max(4, width-4))
	if m.input.Width() != previousWidth {
		m.syncComposerViewport()
	}
	m.input.SetHeight(min(m.input.Height(), max(1, min(4, m.height/3, m.height-chrome-1-boolInt(len(m.attachments) > 0)))))
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

// SetValue and width changes recalculate height before refreshing Bubbles'
// viewport content. Synchronize it before clamping height so scrolling uses the
// new wrapped rows, not an empty or differently sized previous render.
func (m *Model) syncComposerViewport() {
	focused := m.input.Focused()
	m.input.Focus()
	m.input, _ = m.input.Update(nil)
	if !focused {
		m.input.Blur()
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (m *Model) refresh() {
	follow := m.viewport.AtBottom() && m.textSelection == nil
	text, positions := m.renderer.Render(m.state.Active(), transcript.Options{Width: m.viewport.Width(), Selected: m.selected, Focused: m.focusedTranscript, Thoughts: m.opts.Settings.ShowThoughts, Compact: m.opts.Settings.Compact, Search: m.searchQuery, SearchOccurrence: m.searchOccurrence, Expanded: m.expanded, Terminals: m.state.TerminalOutputs, SpinnerFrame: m.toolSpinnerFrame, ReducedMotion: m.opts.Settings.ReducedMotion})
	m.positions = positions
	m.validateTextSelection(text)
	m.transcriptContent = text
	m.setTranscriptContent()
	if follow && !m.focusedTranscript {
		m.viewport.GotoBottom()
		m.unseen = false
	}
}

func (m *Model) send() tea.Cmd {
	text := expandPastes(strings.TrimSpace(m.input.Value()), m.currentDraft().Pastes)
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
	if m.pastingImage || m.pastingText {
		m.state.Notice = "Reading clipboard · draft preserved · send when the paste is ready"
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
	if strings.TrimSpace(text) == "" && len(m.attachments) == 0 {
		return nil
	}
	if len(text) > maxDraftBytes {
		m.state.Notice = "Prompt exceeds 8 MiB · draft preserved"
		return nil
	}
	prompt := harness.Prompt{Text: text, Attachments: append([]harness.Attachment(nil), m.attachments...)}
	if c == nil {
		m.pendingFirstPrompt, m.pendingFirstDraft = &prompt, m.draftText()
		m.sessionPending, m.creatingSession = true, true
		m.state.Notice = "Creating conversation · draft preserved · Ctrl+C to cancel the first send"
		return command(m.opts.Controller.NewSession)
	}
	return m.submitPrompt(prompt, m.draftText())
}

func (m *Model) submitPrompt(prompt harness.Prompt, draft string) tea.Cmd {
	c := m.state.Active()
	id, controller := c.Info.ID, m.opts.Controller
	m.submitting = true
	buffer := m.currentDraft()
	buffer.Submission = &submissionSnapshot{Draft: draft, Prompt: harness.Prompt{Text: prompt.Text, Attachments: append([]harness.Attachment(nil), prompt.Attachments...)}}
	m.history = append(m.history, prompt.Text)
	m.historyIndex = len(m.history)
	if m.draftText() == draft {
		m.input.Reset()
	}
	c.Draft, c.DraftLoaded = m.draftText(), true
	m.attachments = m.attachments[min(len(prompt.Attachments), len(m.attachments)):]
	m.stashDraft()
	m.focusedTranscript = false
	m.input.Focus()
	m.viewport.GotoBottom()
	return command(func() { controller.Prompt(id, prompt) })
}
