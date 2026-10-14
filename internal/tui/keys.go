package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"agents/internal/harness"
	"agents/internal/tui/transcript"

	bubbleskey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type actionSpec struct{ ID, Label, Key string }

var actionCatalog = []actionSpec{
	{"new_session", "New conversation", "ctrl+n"},
	{"sessions", "Session history", "ctrl+s"},
	{"options", "Model, effort, and mode", "ctrl+o"},
	{"palette", "Command palette", "ctrl+p"},
	{"commands", "Slash commands", ""},
	{"skills", "Load a skill", ""},
	{"files", "Attach files", "ctrl+f"},
	{"paste", "Paste clipboard image or text", "ctrl+v"},
	{"paste_image", "Paste clipboard image", ""},
	{"pastes", "Review pasted text blocks", "alt+p"},
	{"clear_attachments", "Remove attachments", ""},
	{"attachments", "Review pending attachments", ""},
	{"plan", "Agent plan", ""},
	{"changes", "Review reported changes", ""},
	{"search", "Search transcript", ""},
	{"history", "Search prompt history", "ctrl+r"},
	{"thoughts", "Toggle reasoning visibility", ""},
	{"effort", "Cycle thinking effort", "ctrl+t"},
	{"compact", "Toggle compact layout", "ctrl+x"},
	{"copy", "Copy selected text or last response", "ctrl+y"},
	{"editor", "Edit draft in external editor", "ctrl+g"},
	{"inspect", "Inspect selected entry", ""},
	{"code", "Copy a code block", ""},
	{"open_file", "Open reported file in editor", ""},
	{"export", "Inspect selected output in external editor", ""},
	{"restore_draft", "Restore cleared draft", ""},
	{"recover", "Restore failed submission", ""},
	{"diagnostics", "Connection and error details", ""},
	{"settings", "Interface settings", ""},
	{"harness", "Choose harness", ""},
	{"auth", "Agent authentication", ""},
	{"reconnect", "Reconnect agent", "alt+r"},
	{"cancel", "Cancel turn", ""},
	{"close_session", "Close current session (keeps history)", ""},
	{"help", "Help and shortcuts", ""},
	{"quit", "Quit", "ctrl+d"},
}

var defaultBindings = func() map[string]string {
	bindings := make(map[string]string)
	for _, action := range actionCatalog {
		bindings[action.ID] = action.Key
	}
	return bindings
}()

func (m *Model) binding(action string) string {
	key := defaultBindings[action]
	if value, ok := m.opts.Settings.Keybindings[action]; ok {
		key = value
	}
	if key == "" {
		return "—"
	}
	parts := strings.Split(key, "+")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "+")
}

func (m *Model) actionFor(key string) string {
	// Explicit remaps take precedence over defaults. Stable ordering prevents
	// collisions in a user keymap from depending on Go's map iteration order.
	var actions []string
	for action := range defaultBindings {
		actions = append(actions, action)
	}
	slices.Sort(actions)
	for _, action := range actions {
		if binding, ok := m.opts.Settings.Keybindings[action]; ok && binding != "" && key == binding {
			return action
		}
	}
	for _, action := range actions {
		if _, overridden := m.opts.Settings.Keybindings[action]; !overridden && defaultBindings[action] != "" && key == defaultBindings[action] {
			return action
		}
	}
	return ""
}

func (m *Model) handleKey(key tea.KeyPressMsg) tea.Cmd {
	pressed := key.String()
	if pressed == "ctrl+c" {
		if m.interaction != nil {
			m.answer(m.interaction, harness.Decision{Action: "cancel"})
			return nil
		}
		if m.list != nil || m.overlay != "" {
			if m.overlay == "commands" {
				m.dismissCommandPicker()
			} else {
				m.closeOverlay()
			}
			return nil
		}
		if m.creatingSession && m.pendingFirstPrompt != nil {
			return m.cancelTurn()
		}
		if m.pendingSetup != nil {
			return m.cancelTurn()
		}
		if c := m.state.Active(); c != nil && c.Busy {
			return m.cancelTurn()
		}
		if m.input.Value() != "" {
			m.clearedDraft = m.draftText()
			m.input.Reset()
			m.state.Notice = "Draft cleared · restore with /restore"
			return nil
		}
		m.state.Notice = m.binding("quit") + " to quit"
		return nil
	}
	if m.interaction != nil {
		return m.interactionKey(key)
	}
	if m.list != nil {
		switch pressed {
		case "esc":
			if m.overlay == "commands" {
				m.dismissCommandPicker()
			} else {
				m.backOverlay()
			}
			return nil
		case "enter":
			if m.list.Error != "" {
				return m.retryOverlay()
			}
			if m.list.Loading {
				return nil
			}
			if m.overlay == "files" && len(m.list.MarkedItems()) > 0 {
				var cmds []tea.Cmd
				for _, item := range m.list.MarkedItems() {
					cmds = append(cmds, m.attach(item.ID))
				}
				m.closeOverlay()
				return tea.Batch(cmds...)
			}
			if item, ok := m.list.Selected(); ok {
				return m.choose(item)
			}
			if m.overlay == "commands" && strings.TrimSpace(m.list.Search.Value()) != "" {
				m.insertSlashCommand(m.commandQueryInvocation())
				m.closeOverlay()
			}
			return nil
		case "delete", "ctrl+x":
			if m.overlay == "pastes" {
				if item, ok := m.list.Selected(); ok {
					m.removePaste(item.Value.(pastedText))
				}
				return nil
			}
			if m.overlay == "attachments" {
				if item, ok := m.list.Selected(); ok {
					m.removeAttachment(item.Value.(int))
				}
				return nil
			}
			if m.overlay == "sessions" && m.state.Peer.Capabilities.Delete {
				if item, ok := m.list.Selected(); ok {
					m.confirmDelete(item)
					return nil
				}
			}
		}
		return m.list.Update(key)
	}
	if m.overlay == "info" {
		if pressed == "esc" {
			m.backOverlay()
			return nil
		}
		switch pressed {
		case "e":
			return m.openReportedFile()
		case "J":
			m.jumpHunk(1)
			return nil
		case "K":
			m.jumpHunk(-1)
			return nil
		case "ctrl+d":
			m.info.SetYOffset(m.info.YOffset() + max(1, m.info.Height()/2))
			return nil
		case "ctrl+u":
			m.info.SetYOffset(m.info.YOffset() - max(1, m.info.Height()/2))
			return nil
		case "G":
			m.info.GotoBottom()
			return nil
		case "g":
			if m.browsePrefix == "g" && time.Since(m.browsePrefixAt) < time.Second {
				m.info.GotoTop()
				m.browsePrefix = ""
			} else {
				m.browsePrefix, m.browsePrefixAt = "g", time.Now()
			}
			return nil
		}
		var cmd tea.Cmd
		m.info, cmd = m.info.Update(key)
		return cmd
	}
	if m.focusedTranscript && m.opts.Settings.VimNavigation && (pressed == "ctrl+d" || pressed == "ctrl+u") {
		m.scrollTranscript(pressed == "ctrl+d")
		return nil
	}
	if action := m.actionFor(pressed); action != "" {
		return m.perform(action)
	}
	if m.textSelection != nil {
		if pressed == "esc" {
			m.clearTextSelection()
			return nil
		}
		if pressed == "y" && m.hasTextSelection() {
			return m.copyTextSelection()
		}
	}
	switch pressed {
	case "tab":
		if !m.focusedTranscript && strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
			m.openCommands()
			return nil
		}
		if !m.focusedTranscript && strings.HasPrefix(lastWord(m.input.Value()), "@") {
			return m.openFiles(strings.TrimPrefix(lastWord(m.input.Value()), "@"))
		}
		return m.setTranscriptFocus(!m.focusedTranscript)
	case "shift+tab":
		return m.cycleMode()
	case "ctrl+home":
		m.viewport.GotoTop()
		if m.focusedTranscript {
			m.selected, m.browseLine, m.dirty = 0, 0, true
		}
		return nil
	case "ctrl+end":
		m.viewport.GotoBottom()
		if m.focusedTranscript {
			if c := m.state.Active(); c != nil {
				m.selected = max(0, len(c.Entries)-1)
			}
			m.browseLine, m.dirty = max(0, len(strings.Split(m.viewport.GetContent(), "\n"))-1), true
		}
		m.unseen = false
		return nil
	case "pgup", "pgdown":
		delta := m.viewport.Height()
		if pressed == "pgup" {
			delta = -delta
		}
		m.scrollTranscriptLines(delta)
		return nil
	case "alt+up":
		m.navigateHistory(-1)
		return nil
	case "alt+down":
		m.navigateHistory(1)
		return nil
	case "esc":
		return m.setTranscriptFocus(m.opts.Settings.VimNavigation)
	case "enter":
		if !m.focusedTranscript {
			return m.send()
		}
		return m.expandSelected()
	}
	if m.focusedTranscript {
		if cmd, handled := m.browseKey(pressed); handled {
			return cmd
		}
		switch pressed {
		case "up":
			m.moveSelection(-1)
		case "down":
			m.moveSelection(1)
		case "space":
			return m.expandSelected()
		case "y":
			return m.copySelected()
		}
		return nil
	}
	var cmd tea.Cmd
	m.clearTextSelection()
	if bubbleskey.Matches(key, m.input.KeyMap.Paste) {
		return m.pasteClipboardText()
	}
	before := m.input.Value()
	cmd = m.updateComposer(key)
	// Only inserting a slash opens automatic completion. Deleting back to a
	// bare slash after dismissing the picker must remain an ordinary edit.
	if (pressed == "/" || key.Text == "/") && before != m.input.Value() && strings.TrimSpace(m.input.Value()) == "/" {
		m.openCommands()
	}
	return cmd
}

func (m *Model) perform(action string) tea.Cmd {
	if reason := m.actionUnavailable(action); reason != "" {
		m.state.Notice = reason
		return nil
	}
	m.recentActions = append([]string{action}, m.recentActions...)
	if len(m.recentActions) > 20 {
		m.recentActions = m.recentActions[:20]
	}
	switch action {
	case "palette":
		m.openPalette()
	case "sessions":
		return m.openSessions()
	case "options":
		return m.openOptions()
	case "new_session":
		return m.newSession()
	case "reconnect":
		return m.reconnect()
	case "thoughts":
		m.opts.Settings.ShowThoughts = !m.opts.Settings.ShowThoughts
		m.dirty = true
		return m.save()
	case "effort":
		return m.cycleEffort()
	case "compact":
		return m.toggleCompact()
	case "copy":
		return m.copyLast()
	case "editor":
		return m.externalEditor()
	case "files":
		return m.openFiles("")
	case "paste":
		return m.pasteClipboardImage(true)
	case "paste_image":
		return m.pasteClipboardImage(false)
	case "pastes":
		m.openPastes()
	case "quit":
		return m.requestQuit()
	case "cancel":
		return m.cancelTurn()
	case "plan":
		m.openPlan()
	case "changes":
		m.openChanges()
	case "settings":
		m.openSettings()
	case "harness":
		m.openHarnesses()
	case "commands":
		m.openCommands()
	case "skills":
		return m.openSkills("")
	case "auth":
		m.openAuth()
	case "clear_attachments":
		m.clearAttachments()
	case "attachments":
		m.openAttachments()
	case "recover":
		return m.recoverSubmission()
	case "diagnostics":
		m.openDiagnostics()
	case "close_session":
		if c := m.state.Active(); c != nil && !c.Busy && m.state.Peer.Capabilities.Close {
			id, controller := c.Info.ID, m.opts.Controller
			return command(func() { controller.CloseSession(id) })
		}
	case "help":
		m.openHelp()
	case "search":
		m.openTranscriptSearch()
	case "history":
		m.openHistory()
	case "inspect":
		m.inspectSelected()
	case "code":
		m.openCodeBlocks()
	case "open_file":
		return m.openReportedFile()
	case "export":
		return m.exportSelected()
	case "restore_draft":
		if m.clearedDraft != "" {
			if m.input.Value() != "" {
				m.state.Notice = "Draft not empty · clear or save it before restoring"
				return nil
			}
			if !m.setDraftText(m.clearedDraft) {
				return nil
			}
			m.clearedDraft = ""
			return m.setTranscriptFocus(false)
		}
		m.state.Notice = "No cleared draft to restore"
	}
	return nil
}

func (m *Model) requestQuit() tea.Cmd {
	if c := m.state.Active(); c != nil && c.Busy {
		if time.Since(m.quitAt) > 3*time.Second {
			m.quitAt = time.Now()
			m.state.Notice = "A turn is running. Repeat /quit or press " + m.binding("quit") + " within 3s to quit, or Ctrl+C to cancel."
			return nil
		}
	}
	return tea.Quit
}

func (m *Model) cancelTurn() tea.Cmd {
	if m.pendingSetup != nil {
		m.pendingSetup = nil
		m.state.Notice = "Configuration cancelled · draft preserved; an empty session may still be created"
		return nil
	}
	if m.creatingSession && m.pendingFirstPrompt != nil {
		m.pendingFirstPrompt = nil
		m.pendingFirstDraft = ""
		m.state.Notice = "First send cancelled · draft preserved"
		return nil
	}
	c := m.state.Active()
	if c == nil || !c.Busy {
		m.state.Notice = "No turn is running."
		return nil
	}
	if c.Cancelling {
		m.state.Notice = "Still cancelling. Use " + m.binding("reconnect") + " to explicitly restart the connection."
		return nil
	}
	c.Cancelling = true
	id, controller := c.Info.ID, m.opts.Controller
	return command(func() { controller.Cancel(id) })
}

func (m *Model) cycleMode() tea.Cmd {
	c := m.state.Active()
	if c == nil {
		return m.prepareSessionSetup(sessionSetupRequest{category: "mode", name: "mode"})
	}
	for _, option := range c.Options {
		if option.Category != "mode" || len(option.Choices) == 0 {
			continue
		}
		index := -1
		for i, choice := range option.Choices {
			if choice.Value == option.Value {
				index = i
				break
			}
		}
		value := option.Choices[(index+1)%len(option.Choices)].Value
		return m.setSessionOption(option, value)
	}
	m.state.Notice = "This agent does not advertise selectable modes."
	return nil
}

func (m *Model) cycleEffort() tea.Cmd {
	c := m.state.Active()
	if c == nil {
		return m.prepareSessionSetup(sessionSetupRequest{category: "thought_level", name: "effort"})
	}
	if m.effortPending || m.configPending {
		m.state.Notice = "Thinking effort change pending · please wait"
		return nil
	}
	if m.state.Connection != "ready" || m.sessionPending || c.Loading {
		m.state.Notice = "Session not ready · thinking effort unchanged"
		return nil
	}
	for _, option := range c.Options {
		if option.Category != "thought_level" || option.Type != "select" || len(option.Choices) == 0 {
			continue
		}
		if len(option.Choices) == 1 {
			m.state.Notice = "Only one thinking effort level is available: " + option.Choices[0].Name
			return nil
		}
		index := -1
		for i, choice := range option.Choices {
			if choice.Value == option.Value {
				index = i
				break
			}
		}
		choice := option.Choices[(index+1)%len(option.Choices)]
		m.effortPending = true
		m.state.Notice = "Changing thinking effort to " + choice.Name
		return m.setSessionOption(option, choice.Value)
	}
	m.state.Notice = "This agent/model does not advertise selectable thinking effort levels"
	return nil
}

func (m *Model) moveSelection(delta int) {
	c := m.state.Active()
	if c == nil {
		return
	}
	m.selected = min(max(0, m.selected+delta), max(0, len(c.Entries)-1))
	m.refresh()
	if m.selected < len(m.positions) {
		line := m.positions[m.selected]
		if line < m.viewport.YOffset() || line >= m.viewport.YOffset()+m.viewport.Height() {
			m.viewport.SetYOffset(line)
		}
		m.browseLine = min(max(line, m.viewport.YOffset()), m.viewport.YOffset()+m.viewport.Height()-1)
	}
	if m.viewport.AtBottom() {
		m.unseen = false
	}
}

func (m *Model) expandSelected() tea.Cmd {
	entry := m.selectedEntry()
	if entry == nil {
		return nil
	}
	if entry.Tool == nil && entry.Role != "thought" && !(entry.Role == "user" && transcript.LargeText(entry.Text)) {
		m.state.Notice = "Select a tool, thinking block or long user message to expand · ↑↓ select entries"
		return nil
	}
	key := entry.ID
	expanded := m.expanded[key]
	if entry.Role == "thought" {
		key += fmt.Sprint(m.selected)
		var overridden bool
		expanded, overridden = m.expanded[key]
		if !overridden {
			expanded = m.opts.Settings.ShowThoughts
		}
	} else if entry.Role == "user" {
		key += fmt.Sprint(m.selected)
		expanded = m.expanded[key]
	}
	m.expanded[key] = !expanded
	m.dirty = true
	// A collapse may remove the reading line. Anchor the same header at its
	// current screen row rather than letting the selection drift to a neighbor.
	screenRow := 0
	if m.selected < len(m.positions) {
		screenRow = max(0, m.positions[m.selected]-m.viewport.YOffset())
	}
	m.refresh()
	if m.selected < len(m.positions) {
		m.viewport.SetYOffset(m.positions[m.selected] - screenRow)
		m.browseLine = m.positions[m.selected]
	}
	return nil
}

func (m *Model) copySelected() tea.Cmd {
	if m.hasTextSelection() {
		return m.copyTextSelection()
	}
	c := m.state.Active()
	if c == nil || m.selected >= len(c.Entries) {
		return nil
	}
	entry := c.Entries[m.selected]
	text := entry.Text
	if entry.Tool != nil {
		text = entry.Tool.Title + "\n" + entry.Tool.Text
	}
	return m.copyText(text, "Entry")
}

func (m *Model) copyLast() tea.Cmd {
	if m.hasTextSelection() {
		return m.copyTextSelection()
	}
	c := m.state.Active()
	if c == nil {
		return nil
	}
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if c.Entries[i].Role == "assistant" {
			return m.copyText(c.Entries[i].Text, "Response")
		}
	}
	m.state.Notice = "No assistant response to copy yet."
	return nil
}

func (m *Model) navigateHistory(delta int) {
	if len(m.history) == 0 {
		return
	}
	if m.historyIndex == len(m.history) {
		m.draftBeforeHistory = m.draftText()
	}
	m.historyIndex = min(max(0, m.historyIndex+delta), len(m.history))
	if m.historyIndex == len(m.history) {
		m.setDraftText(m.draftBeforeHistory)
	} else {
		m.setDraftText(m.history[m.historyIndex])
	}
}

func lastWord(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}
