package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/tui/overlays"
	"agents/internal/tui/theme"
	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type overlayFrame struct {
	List *overlays.List
	Kind string
}

func (m *Model) closeOverlay() {
	m.list, m.overlay, m.infoTitle = nil, "", ""
	m.infoPaths, m.infoHunks, m.infoContent = nil, nil, ""
	m.browsePrefix = ""
	m.overlayStack = nil
	if !m.focusedTranscript {
		m.input.Focus()
	}
}

func (m *Model) openList(kind, title string, items []overlays.Item) {
	m.overlay, m.list = kind, overlays.NewList(title, items, true)
	m.overlayStack = nil
	m.input.Blur()
}

func (m *Model) backOverlay() {
	if len(m.overlayStack) == 0 {
		m.closeOverlay()
		return
	}
	frame := m.overlayStack[len(m.overlayStack)-1]
	m.list, m.overlay = frame.List, frame.Kind
	m.overlayStack = m.overlayStack[:len(m.overlayStack)-1]
	m.infoTitle, m.infoContent, m.infoPaths, m.infoHunks = "", "", nil, nil
	m.input.Blur()
}

func selectListID(list *overlays.List, id string) {
	for index, item := range list.Filtered() {
		if item.ID == id {
			list.Cursor = index
			return
		}
	}
}

func (m *Model) openPalette() {
	var items []overlays.Item
	for _, action := range actionCatalog {
		group := actionGroup(action.ID)
		items = append(items, overlays.Item{ID: action.ID, Label: action.Label, Detail: m.binding(action.ID), Group: group, DisabledReason: m.actionUnavailable(action.ID), Preview: action.Label + "\n\n" + group + " · " + m.binding(action.ID)})
	}
	slices.SortStableFunc(items, func(a, b overlays.Item) int {
		rank := func(id string) int {
			if index := slices.Index(m.recentActions, id); index >= 0 {
				return index
			}
			return len(m.recentActions)
		}
		if delta := rank(a.ID) - rank(b.ID); delta != 0 {
			return delta
		}
		groups := []string{"Conversation", "Agent", "Attachments", "Review", "Interface"}
		return slices.Index(groups, a.Group) - slices.Index(groups, b.Group)
	})
	m.openList("palette", "Commands", items)
}

func (m *Model) openSessions() tea.Cmd {
	if !m.state.Peer.Capabilities.List {
		m.state.Notice = "This agent does not advertise session listing."
		return nil
	}
	m.openList("sessions", "Session history", nil)
	m.list.Loading = true
	m.list.Description = "Loading sessions…"
	m.list.EmptyMessage = "No saved conversations. Send a task to start one."
	controller := m.opts.Controller
	return command(controller.ListSessions)
}

func (m *Model) populateSessions() {
	if m.list == nil {
		return
	}
	selectedID := ""
	if item, ok := m.list.Selected(); ok {
		selectedID = item.ID
	}
	cursor, scroll := m.list.Cursor, m.list.Scroll
	m.list.Items = nil
	m.list.Loading = false
	m.list.Error = ""
	m.list.Description = "Enter loads history."
	if m.state.Peer.Capabilities.Delete {
		m.list.Description += " Ctrl+X deletes after confirmation."
	}
	m.list.Hint = "Enter load · Esc close"
	m.list.GroupHeaders = true
	sessions := append([]harness.SessionInfo(nil), m.state.Sessions...)
	slices.SortStableFunc(sessions, func(a, b harness.SessionInfo) int {
		at, aErr := time.Parse(time.RFC3339Nano, a.UpdatedAt)
		bt, bErr := time.Parse(time.RFC3339Nano, b.UpdatedAt)
		if aErr != nil && bErr != nil {
			return 0
		}
		if aErr != nil {
			return 1
		}
		if bErr != nil {
			return -1
		}
		return bt.Compare(at)
	})
	for _, info := range sessions {
		label := info.Title
		if label == "" {
			label = info.ID
		}
		current := info.ID == m.state.ActiveID
		detail := ""
		if at, err := time.Parse(time.RFC3339Nano, info.UpdatedAt); err == nil {
			detail = at.Local().Format("15:04")
		}
		m.list.Items = append(m.list.Items, overlays.Item{ID: info.ID, Label: label, Detail: detail, Group: sessionDay(info.UpdatedAt), Keywords: info.ID + " " + info.Workspace, Current: current, Preview: label + "\n\nWorkspace: " + info.Workspace + "\nUpdated: " + sessionDate(info.UpdatedAt) + "\nSession: " + info.ID, Value: info})
	}
	m.list.Cursor, m.list.Scroll = cursor, scroll
	selectListID(m.list, selectedID)
}

func sessionDate(value string) string {
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at.Local().Format("Jan 2, 2006, 15:04")
	}
	return "Unknown date"
}

func sessionDay(value string) string {
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at.Local().Format("January 2, 2006")
	}
	return "Unknown date"
}

func (m *Model) confirmDelete(item overlays.Item) {
	parent := overlayFrame{m.list, m.overlay}
	history := append([]overlayFrame(nil), m.overlayStack...)
	m.openList("delete", "Delete saved session?", []overlays.Item{{ID: "no", Label: "Keep session"}, {ID: "yes", Label: "Delete permanently", Value: item.Value}})
	m.overlayStack = append(history, parent)
	m.list.Searchable = false
	m.list.Description = item.Label + " · This cannot be undone."
}

func (m *Model) openOptions() tea.Cmd {
	c := m.state.Active()
	if c == nil {
		return m.prepareSessionSetup(sessionSetupRequest{})
	}
	if m.sessionPending || c.Loading || m.state.Connection != "ready" {
		m.state.Notice = "Session not ready · configuration unavailable"
		return nil
	}
	m.openList("options", "Session options", nil)
	m.populateOptions()
	m.list.Description = "Options come from the agent. Model changes may update other controls."
	m.list.EmptyMessage = "This agent has not advertised session options."
	return nil
}

func optionValueName(option harness.ConfigOption) string {
	for _, choice := range option.Choices {
		if reflect.DeepEqual(choice.Value, option.Value) {
			return choice.Name
		}
	}
	if value, ok := option.Value.(bool); ok {
		return onOff(value)
	}
	return fmt.Sprint(option.Value)
}

func (m *Model) populateOptions() {
	if m.list == nil || m.state.Active() == nil {
		return
	}
	selectedID := ""
	if item, ok := m.list.Selected(); ok {
		selectedID = item.ID
	}
	m.list.Items = nil
	for _, option := range m.state.Active().Options {
		m.list.Items = append(m.list.Items, overlays.Item{ID: option.ID, Label: option.Name, Detail: optionValueName(option), Preview: option.Description + "\n\nCurrent: " + optionValueName(option), Value: option})
	}
	selectListID(m.list, selectedID)
}

func (m *Model) openFiles(query string) tea.Cmd {
	m.filePickerGeneration++
	m.filePreviewPath = ""
	generation := m.filePickerGeneration
	m.openList("files", "Attach workspace file", nil)
	m.list.MultiSelect = true
	m.list.Loading = true
	m.list.Search.SetValue(query)
	m.list.Description = "Loading files… File references are sent as ACP resources."
	workspace := m.opts.Agent.Workspace
	if c := m.state.Active(); c != nil {
		workspace = c.Info.Workspace
	}
	ctx := m.opts.Context
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		files, err := app.IndexFiles(ctx, workspace)
		return filesMsg{files: files, err: err, generation: generation}
	}
}

func (m *Model) openInfo(title, content string) {
	m.closeOverlay()
	m.overlay, m.infoTitle = "info", title
	m.infoContent = content
	m.sizeInfo()
	wrapped := lipgloss.Wrap(content, m.info.Width(), " ")
	m.info.SoftWrap = false
	m.info.SetContent(wrapped)
	for index, line := range strings.Split(wrapped, "\n") {
		if strings.HasPrefix(theme.Clean(line), "@@") {
			m.infoHunks = append(m.infoHunks, index)
		}
	}
	m.info.GotoTop()
	m.input.Blur()
}

func (m *Model) openPlan() {
	c := m.state.Active()
	if c == nil {
		return
	}
	var lines []string
	for _, entry := range c.Plan {
		icon := "○"
		if entry.Status == "completed" {
			icon = "✓"
		}
		if entry.Status == "in_progress" {
			icon = "◌"
		}
		lines = append(lines, theme.Body.Render(icon+" "+theme.Clean(entry.Content))+theme.Quiet.Render("  "+entry.Status))
	}
	if len(lines) == 0 {
		lines = []string{"This agent has not reported a plan."}
	}
	m.openInfo("Agent plan", strings.Join(lines, "\n\n"))
}

func (m *Model) openChanges() {
	c := m.state.Active()
	if c == nil {
		return
	}
	var items []overlays.Item
	byPath := make(map[string][]reportedChange)
	var paths []string
	reports := 0
	for index, entry := range c.Entries {
		if entry.Tool != nil {
			for _, diff := range entry.Tool.Diffs {
				if _, exists := byPath[diff.Path]; !exists {
					paths = append(paths, diff.Path)
				}
				byPath[diff.Path] = append(byPath[diff.Path], reportedChange{Entry: index, Diff: diff})
				reports++
			}
		}
	}
	slices.Sort(paths)
	for _, path := range paths {
		changes := byPath[path]
		var value any = changes
		detail := fmt.Sprintf("%d reports", len(changes))
		preview := fmt.Sprintf("%d separate reported edits to %s. Enter lists each report.", len(changes), path)
		if len(changes) == 1 {
			value, preview = changes[0], transcript.RenderDiff(changes[0].Diff)
			detail = "1 report · " + c.Entries[changes[0].Entry].Tool.Status
		}
		items = append(items, overlays.Item{ID: path, Label: theme.Clean(path), Detail: detail, Preview: preview, Value: value})
	}
	m.openList("changes", "Reported changes", items)
	m.list.Description = fmt.Sprintf("%d files · %d reports. Agent previews, not current working-tree diffs. Enter inspects; repeated edits remain separate.", len(paths), reports)
	m.list.EmptyMessage = "The agent has not reported any structured diffs."
}

func (m *Model) openSettings() {
	m.openList("settings", "Interface settings", []overlays.Item{
		{ID: "mouse", Label: "Mouse scrolling, text selection, and links", Detail: onOff(m.opts.Settings.Mouse)},
		{ID: "thoughts", Label: "Expand reasoning", Detail: onOff(m.opts.Settings.ShowThoughts)},
		{ID: "vim", Label: "Vim navigation (Esc browse, i compose)", Detail: onOff(m.opts.Settings.VimNavigation)},
		{ID: "compact", Label: "Compact layout", Detail: onOff(m.opts.Settings.Compact)},
		{ID: "appearance", Label: "Appearance", Detail: appearanceName(m.opts.Settings.Appearance), Preview: "Enter cycles Graphite, High contrast, and Terminal default (no application colors)."},
		{ID: "motion", Label: "Reduced motion", Detail: onOff(m.opts.Settings.ReducedMotion)},
	})
	m.list.Hint = "Enter toggle · Esc close"
	if m.opts.Store != nil {
		m.list.Description = "Stored in " + filepath.Join(m.opts.Store.Directory, "settings.json") + ". Keybindings can also be customized there."
	}
}

func onOff(value bool) string {
	if value {
		return "On"
	}
	return "Off"
}

func appearanceName(value string) string {
	switch value {
	case "high-contrast":
		return "High contrast"
	case "terminal":
		return "Terminal default"
	default:
		return "Graphite"
	}
}

func (m *Model) toggleSetting(item overlays.Item) tea.Cmd {
	query := m.list.Search.Value()
	var cmd tea.Cmd
	switch item.ID {
	case "mouse":
		m.clearTextSelection()
		m.opts.Settings.Mouse = !m.opts.Settings.Mouse
	case "thoughts":
		m.opts.Settings.ShowThoughts = !m.opts.Settings.ShowThoughts
	case "vim":
		m.opts.Settings.VimNavigation = !m.opts.Settings.VimNavigation
	case "compact":
		cmd = m.toggleCompact()
	case "motion":
		m.opts.Settings.ReducedMotion = !m.opts.Settings.ReducedMotion
	case "appearance":
		switch m.opts.Settings.Appearance {
		case "high-contrast":
			m.opts.Settings.Appearance = "terminal"
		case "terminal":
			m.opts.Settings.Appearance = ""
		default:
			m.opts.Settings.Appearance = "high-contrast"
		}
	}
	m.dirty = true
	m.openSettings()
	m.list.Search.SetValue(query)
	selectListID(m.list, item.ID)
	if cmd == nil {
		cmd = m.save()
	}
	return cmd
}

func (m *Model) openHarnesses() {
	var items []overlays.Item
	for _, descriptor := range m.opts.Registry.List() {
		items = append(items, overlays.Item{ID: descriptor.ID, Label: descriptor.Name, Detail: descriptor.Description, Value: descriptor})
	}
	m.openList("harnesses", "Choose harness", items)
	m.list.Description = "Each harness owns its conversations. Switching does not transfer context."
}

func (m *Model) openAuth() {
	var items []overlays.Item
	for _, method := range m.state.Peer.Auth {
		items = append(items, overlays.Item{ID: method.ID, Label: method.Name, Detail: method.Description, Value: method})
	}
	m.openList("auth", "Agent authentication", items)
	m.list.Description = m.opts.Descriptor.LoginHint
	m.list.EmptyMessage = "No login flows advertised. See the setup instructions above."
	if len(items) == 0 && m.opts.Descriptor.LoginHint != "" {
		m.list.Items = []overlays.Item{{ID: "setup", Label: "Sign-in setup instructions", Preview: m.opts.Descriptor.LoginHint}}
	}
}

func (m *Model) openHelp() {
	var lines []string
	for _, action := range actionCatalog {
		if m.binding(action.ID) != "—" {
			lines = append(lines, fmt.Sprintf("%-14s %s", m.binding(action.ID), action.Label))
		}
	}
	lines = append(lines, `
Ctrl+C       Dismiss overlay / cancel interaction / stop turn / clear draft
/restore     Restore the last cleared draft (does not overwrite text)
Enter        Send or run a slash command
Alt+Enter / Ctrl+J / Shift+Enter   Insert newline
Shift+Tab    Cycle agent-provided mode
Alt+↑ / ↓    Prompt history (Alt+Down restores the previous draft)
Tab          Switch composer/transcript, complete /commands or @files
PgUp/PgDn    Scroll conversation

Browse mode:
j/k          Scroll transcript by visual line
↑/↓          Select entries
i            Compose, preserving the draft cursor
gg / G       First / latest entry
J / K        Next / previous turn
] / [        Next / previous failed tool
} / {        Next / previous reported edit
/            Search transcript; n/N repeat
Enter/Space  Expand tool or thought
o            Open entry detail pager
y / c        Copy highlighted text (or entry) / choose code block
e / v        Open reported file / export output to editor
?            Help

With Vim navigation enabled: Esc enters browse; Ctrl+D/U half-page scroll.
Mouse: click a transcript link to open it in the browser.
Left-drag text to highlight; right-click or y copies it.
Esc clears text highlighting. Copy uses terminal OSC52 clipboard support.
Detail pager: j/k scroll · Ctrl+D/U half-page · gg/G top/bottom · J/K hunks.
Pickers: type to filter · Ctrl+N/P select · Tab marks files · Esc closes.

Only reported usage is displayed. Reported diffs are not current working-tree
changes and are never reapplied. Local agents are trusted, not sandboxed.`)
	m.openInfo("Agents · keyboard-first ACP client", strings.Join(lines, "\n")+"\n\n"+slashHelp())
}

func (m *Model) choose(item overlays.Item) tea.Cmd {
	kind := m.overlay
	if item.DisabledReason != "" {
		m.list.Description = item.DisabledReason
		m.state.Notice = item.DisabledReason
		return nil
	}
	if kind == "settings" {
		return m.toggleSetting(item)
	}
	history := append([]overlayFrame(nil), m.overlayStack...)
	source := m.list
	returnToParent := func() {
		if len(history) > 0 {
			frame := history[len(history)-1]
			m.list, m.overlay = frame.List, frame.Kind
			m.overlayStack = history[:len(history)-1]
			m.input.Blur()
		}
	}
	if kind == "search" {
		m.searchQuery = m.list.Search.Value()
	}
	m.closeOverlay()
	switch kind {
	case "palette":
		return m.perform(item.ID)
	case "sessions":
		if m.sessionPending {
			return nil
		}
		if c := m.state.Active(); c != nil {
			if c.Busy {
				m.state.Notice = "Cancel the active turn before loading another session."
				return nil
			}
			c.Draft = m.draftText()
			c.DraftLoaded = true
		}
		m.stashDraft()
		info := item.Value.(harness.SessionInfo)
		m.sessionPending = true
		controller := m.opts.Controller
		return command(func() { controller.LoadSession(info) })
	case "delete":
		returnToParent()
		if item.ID == "yes" {
			info := item.Value.(harness.SessionInfo)
			if c := m.state.Active(); c != nil && c.Info.ID == info.ID && c.Busy {
				m.state.Notice = "Cancel this session before deleting it."
				return nil
			}
			controller := m.opts.Controller
			m.list.Loading = true
			m.list.Description = "Deleting session…"
			return command(func() { controller.DeleteSession(info.ID) })
		}
	case "options":
		m.openOptionValues(item.Value.(harness.ConfigOption))
		m.overlayStack = append(history, overlayFrame{source, "options"})
	case "values":
		if c := m.state.Active(); c != nil {
			var cmd tea.Cmd
			if !reflect.DeepEqual(m.option.Value, item.Value) {
				cmd = m.setSessionOption(m.option, item.Value)
			}
			returnToParent()
			return cmd
		}
	case "commands":
		selection := item.Value.(slashSelection)
		m.insertSlashCommand(selection.Invocation)
	case "skills":
		return m.stageSkill(item.Value.(app.Skill))
	case "search":
		m.setTranscriptFocus(true)
		m.revealSearch(item.Value.(int))
	case "history":
		if m.historyIndex == len(m.history) {
			m.draftBeforeHistory = m.draftText()
		}
		for index, text := range m.history {
			if text == item.Value.(string) {
				m.historyIndex = index
			}
		}
		m.setDraftText(item.Value.(string))
		return m.setTranscriptFocus(false)
	case "code":
		return m.copyText(item.Value.(string), "Code")
	case "changes", "file_changes":
		if group, ok := item.Value.([]reportedChange); ok {
			var items []overlays.Item
			for index, change := range group {
				tool := m.state.Active().Entries[change.Entry].Tool
				items = append(items, overlays.Item{ID: fmt.Sprint(index), Label: fmt.Sprintf("Report %d · Entry %d", index+1, change.Entry+1), Detail: tool.Status, Preview: transcript.RenderDiff(change.Diff), Value: change})
			}
			m.openList("file_changes", "Reported edits · "+item.Label, items)
			m.list.Description = "Each report is separate. These are previews, not the current working-tree diff."
			m.overlayStack = append(history, overlayFrame{source, kind})
			break
		}
		change := item.Value.(reportedChange)
		m.selected = change.Entry
		status := m.state.Active().Entries[change.Entry].Tool.Status
		m.openInfo("Reported diff · "+theme.Clean(change.Diff.Path)+" · informational, not reapplied", theme.Quiet.Render("Reported tool status: "+theme.Clean(status))+"\n\n"+transcript.RenderDiff(change.Diff))
		m.infoPaths = []string{change.Diff.Path}
		m.overlayStack = append(history, overlayFrame{source, kind})
	case "attachments":
		index := item.Value.(int)
		if index < len(m.attachments) {
			m.openInfo("Attachment details", item.Preview)
			m.overlayStack = append(history, overlayFrame{source, "attachments"})
		}
	case "pastes":
		paste := item.Value.(pastedText)
		m.openInfo(paste.Marker+" · preview", pastePreview(paste.Text, 64<<10))
		m.overlayStack = append(history, overlayFrame{source, "pastes"})
	case "editor_files":
		return m.editReportedPath(item.Value.(string))
	case "files":
		word := lastWord(m.input.Value())
		if strings.HasPrefix(word, "@") {
			m.input.SetValue(strings.TrimSuffix(m.input.Value(), word) + "@" + item.ID + " ")
		}
		return m.attach(item.Value.(string))
	case "harnesses":
		if m.sessionPending {
			m.state.Notice = "Wait for the session operation before switching harness."
			return nil
		}
		if c := m.state.Active(); c != nil && c.Busy {
			m.state.Notice = "Cancel the active turn before switching harness."
			return nil
		}
		settings, snapshot := m.Snapshot()
		m.opts.Snapshot = snapshot
		m.opts.Descriptor = item.Value.(harness.Descriptor)
		m.opts.Settings = settings
		m.opts.Settings.Harness = m.opts.Descriptor.ID
		m.opts.Agent.Launch = m.opts.Descriptor.Launch
		m.opts.Demo = m.opts.Descriptor.ID == "demo"
		generation := m.state.Generation
		m.state = app.NewState()
		// Queued events from the previous harness may still arrive before the
		// next Connecting event. Reject them during the transition as well.
		m.state.Generation = generation + 1
		m.displayedSessionID = ""
		m.input.Reset()
		m.attachments = nil
		m.clipboardGeneration++
		m.pastingImage, m.pastingText = false, false
		m.restoreLocalBuffer()
		m.history = nil
		clear(m.expanded)
		m.demoStarted = false
		return tea.Batch(m.connect(nil), m.save())
	case "auth":
		if item.ID == "setup" && item.Value == nil {
			m.openInfo("Sign-in setup", m.opts.Descriptor.LoginHint+"\n\n"+m.binding("reconnect")+" reconnects after setup.")
			m.overlayStack = append(history, overlayFrame{source, "auth"})
			break
		}
		return m.authenticate(item.Value.(harness.AuthMethod))
	}
	return nil
}

func (m *Model) openOptionValues(option harness.ConfigOption) {
	m.option = option
	var items []overlays.Item
	if option.Type == "boolean" {
		items = []overlays.Item{{ID: "true", Label: "Enabled", Value: true, Current: reflect.DeepEqual(option.Value, true)}, {ID: "false", Label: "Disabled", Value: false, Current: reflect.DeepEqual(option.Value, false)}}
	} else {
		for _, choice := range option.Choices {
			detail := choice.Description
			if choice.Group != "" {
				detail = choice.Group + " · " + detail
			}
			items = append(items, overlays.Item{ID: choice.Value, Label: choice.Name, Detail: detail, Preview: choice.Name + "\n\n" + choice.Description + "\n\nID: " + choice.Value, Current: reflect.DeepEqual(option.Value, choice.Value), Value: choice.Value})
		}
	}
	m.openList("values", option.Name, items)
	m.list.Description = option.Description
	m.list.EmptyMessage = "No selectable values advertised for this option."
	for index, item := range m.list.Items {
		if item.Current {
			m.list.Cursor = index
			break
		}
	}
}
