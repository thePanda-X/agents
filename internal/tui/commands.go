package tui

import (
	"fmt"
	"strings"
	"unicode"

	"agents/internal/harness"
	"agents/internal/tui/overlays"

	tea "charm.land/bubbletea/v2"
)

// Client commands share the existing action handlers with keyboard shortcuts.
// The catalog also drives completion and help so their names stay consistent.
type slashCommand struct {
	Name, Description, Arguments string
	Action, Category             string
	Aliases                      []string
}

var localSlashCommands = []slashCommand{
	{Name: "help", Description: "Show shortcuts and slash commands", Action: "help", Aliases: []string{"?"}},
	{Name: "new", Description: "Start a new conversation", Action: "new_session", Aliases: []string{"clear", "reset", "new-chat"}},
	{Name: "sessions", Description: "Browse saved conversations", Action: "sessions", Aliases: []string{"resume", "session", "conversations"}},
	{Name: "options", Description: "Show all session options", Action: "options"},
	{Name: "model", Description: "Choose an agent-provided model", Arguments: "[value]", Category: "model", Aliases: []string{"models"}},
	{Name: "effort", Description: "Choose the reasoning effort", Arguments: "[value]", Category: "thought_level", Aliases: []string{"reasoning-effort"}},
	{Name: "mode", Description: "Choose the agent mode", Arguments: "[value]", Category: "mode", Aliases: []string{"modes"}},
	{Name: "harness", Description: "Choose a coding harness", Action: "harness", Aliases: []string{"backend"}},
	{Name: "commands", Description: "Browse client and agent slash commands", Action: "commands"},
	{Name: "skills", Description: "Browse installed skills and stage a loading request", Arguments: "[query]", Action: "skills", Aliases: []string{"skill"}},
	{Name: "settings", Description: "Show interface settings", Action: "settings", Aliases: []string{"config", "preferences", "prefs"}},
	{Name: "plan", Description: "View the reported agent plan", Action: "plan"},
	{Name: "changes", Description: "Review reported diffs", Action: "changes", Aliases: []string{"diff", "diffs"}},
	{Name: "attach", Description: "Attach a file, or open the file picker", Arguments: "[path]", Action: "files", Aliases: []string{"add", "file", "files"}},
	{Name: "paste-image", Description: "Attach an image from the local desktop clipboard", Action: "paste_image"},
	{Name: "pastes", Description: "Review or remove collapsed text blocks in the draft", Action: "pastes"},
	{Name: "detach", Description: "Remove pending attachments", Action: "clear_attachments", Aliases: []string{"remove", "unattach"}},
	{Name: "attachments", Description: "Inspect or individually remove pending attachments", Action: "attachments"},
	{Name: "thoughts", Description: "Toggle expanded reasoning", Action: "thoughts", Aliases: []string{"thinking", "reasoning"}},
	{Name: "copy", Description: "Copy selected text or the last response", Action: "copy", Aliases: []string{"yank"}},
	{Name: "editor", Description: "Compose a prompt in your external editor", Action: "editor", Aliases: []string{"edit"}},
	{Name: "search", Description: "Search the transcript", Action: "search", Aliases: []string{"find"}},
	{Name: "history", Description: "Search prompt history", Action: "history"},
	{Name: "restore", Description: "Restore the last cleared draft", Action: "restore_draft", Aliases: []string{"undo-clear"}},
	{Name: "recover", Description: "Restore an interrupted submission for review, without resending", Action: "recover"},
	{Name: "diagnostics", Description: "Inspect connection errors and setup instructions", Action: "diagnostics"},
	{Name: "inspect", Description: "Inspect the selected transcript entry", Action: "inspect", Aliases: []string{"details"}},
	{Name: "auth", Description: "Show agent authentication methods", Action: "auth", Aliases: []string{"login"}},
	{Name: "reconnect", Description: "Reconnect without resending a prompt", Action: "reconnect", Aliases: []string{"connect"}},
	{Name: "cancel", Description: "Cancel the active turn", Action: "cancel", Aliases: []string{"stop", "abort"}},
	{Name: "quit", Description: "Quit (confirm while a turn is running)", Action: "quit", Aliases: []string{"exit", "q", "bye"}},
	{Name: "agent", Description: "Send a command directly to the agent", Arguments: "[command [args]]"},
}

func findLocalCommand(name string) (slashCommand, bool) {
	for _, command := range localSlashCommands {
		if command.Name == name {
			return command, true
		}
		for _, alias := range command.Aliases {
			if alias == name {
				return command, true
			}
		}
	}
	return slashCommand{}, false
}

// Only single-line slash invocations are interpreted locally. Multiline prompts
// and unrecognized commands remain ordinary agent input.
func parseSlashInvocation(text string) (name, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, "\r\n") {
		return "", "", false
	}
	invocation := strings.TrimPrefix(text, "/")
	if index := strings.IndexFunc(invocation, unicode.IsSpace); index >= 0 {
		return invocation[:index], strings.TrimSpace(invocation[index:]), index > 0
	}
	return invocation, "", true
}

// handleSlashCommand returns handled even on invalid arguments: a malformed
// client command must never accidentally become a model prompt.
func (m *Model) handleSlashCommand(text string) (tea.Cmd, bool) {
	name, args, ok := parseSlashInvocation(text)
	if !ok {
		return nil, false
	}
	if name == "" {
		m.openCommands()
		return nil, true
	}
	local, ok := findLocalCommand(name)
	if !ok {
		return nil, false
	}
	if local.Name == "agent" {
		if args != "" {
			// Do not dispatch again: /agent help must reach the harness, even
			// though /help itself is a client command.
			invocation := strings.TrimPrefix(args, "/")
			if invocation == "" {
				m.state.LastError = fmt.Errorf("usage: /agent command [args]")
				return nil, true
			}
			return m.sendPrompt("/" + invocation), true
		}
		m.consumeSlashCommand()
		m.openSlashCommands(true)
		return nil, true
	}
	if local.Category != "" {
		return m.runSlashOption(local, args), true
	}
	if args != "" && local.Name != "attach" && local.Name != "skills" {
		m.state.LastError = fmt.Errorf("/%s takes no arguments; use /agent %s to send it to the harness", name, name)
		return nil, true
	}
	m.consumeSlashCommand()
	if local.Name == "skills" {
		return m.openSkills(args), true
	}
	if local.Name == "attach" && args != "" {
		// The remainder is a literal path, including spaces, not shell source.
		return m.attach(args), true
	}
	return m.perform(local.Action), true
}

func (m *Model) consumeSlashCommand() {
	m.input.Reset()
	m.state.LastError = nil
	if c := m.state.Active(); c != nil {
		c.Draft, c.DraftLoaded = "", true
	}
	// Local actions do not consume attachments, create transcript entries, or
	// become prompt-history items.
}

func (m *Model) runSlashOption(local slashCommand, args string) tea.Cmd {
	c := m.state.Active()
	if c == nil {
		return m.prepareSessionSetup(sessionSetupRequest{category: local.Category, name: local.Name, args: args, invocation: m.input.Value()})
	}
	if c != nil && !c.Loading && !m.sessionPending && m.state.Connection == "ready" {
		for _, option := range c.Options {
			if option.Category != local.Category {
				continue
			}
			if args == "" {
				m.consumeSlashCommand()
				m.openOptionValues(option)
				return nil
			}
			newValue, err := matchOptionChoice(option, args)
			if err != nil {
				m.state.LastError = fmt.Errorf("%w; use /%s to browse available values", err, local.Name)
				return nil
			}
			m.consumeSlashCommand()
			return m.setSessionOption(option, newValue)
		}
	}
	m.state.LastError = fmt.Errorf("/%s is unavailable for the current session", local.Name)
	return nil
}

func matchOptionChoice(option harness.ConfigOption, query string) (string, error) {
	// Stable IDs take precedence over friendly names. Duplicate display names
	// require an ID rather than silently choosing the first model or mode.
	for _, choice := range option.Choices {
		if choice.Value == query {
			return choice.Value, nil
		}
	}
	var matches []string
	for _, choice := range option.Choices {
		if strings.EqualFold(choice.Name, query) {
			matches = append(matches, choice.Value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous %s %q; specify its value ID", option.Name, query)
	}
	return "", fmt.Errorf("unknown %s %q", option.Name, query)
}

type slashSelection struct{ Invocation string }

func (m *Model) slashItems(agentOnly bool) []overlays.Item {
	var items []overlays.Item
	if !agentOnly {
		for _, local := range localSlashCommands {
			label := "/" + local.Name
			if local.Arguments != "" {
				label += " " + local.Arguments
			}
			detail := "Client · " + local.Description
			keywords := strings.Join(local.Aliases, " ")
			preview := local.Description + "\n\nRuns locally. Enter inserts the command; Enter in the composer runs it."
			if keywords != "" {
				preview += "\n\nAliases: /" + strings.Join(local.Aliases, ", /")
			}
			items = append(items, overlays.Item{ID: "client:" + local.Name, Label: label, Detail: detail, Keywords: keywords, Group: "Client", Preview: preview, Value: slashSelection{Invocation: "/" + local.Name}})
		}
	}
	if c := m.state.Active(); c != nil {
		seen := make(map[string]bool)
		for _, advertised := range c.Commands {
			name := strings.TrimPrefix(advertised.Name, "/")
			if name == "" || strings.ContainsFunc(name, unicode.IsSpace) || seen[name] {
				continue
			}
			seen[name] = true
			invocation := "/" + name
			if _, collision := findLocalCommand(name); collision || agentOnly {
				invocation = "/agent " + name
			}
			label := invocation
			if advertised.Hint != "" {
				label += " " + advertised.Hint
			}
			items = append(items, overlays.Item{ID: "agent:" + name, Label: label, Detail: m.opts.Descriptor.Name + " · " + advertised.Description, Group: m.opts.Descriptor.Name, Preview: advertised.Description + "\n\nSent to " + m.opts.Descriptor.Name + ". Enter inserts; Enter in the composer sends.", Value: slashSelection{Invocation: invocation}})
		}
	}
	return items
}

func (m *Model) openCommands() { m.openSlashCommands(false) }

func (m *Model) openSlashCommands(agentOnly bool) {
	m.commandAgentOnly = agentOnly
	title := "Slash commands"
	if agentOnly {
		title = "Agent slash commands"
	}
	m.openList("commands", title, m.slashItems(agentOnly))
	m.list.Hint = "Enter insert · Esc close"
	m.list.Description = "Client commands run locally. Agent commands are sent to the harness. Enter inserts a command; Enter again runs it."
	name, args, ok := parseSlashInvocation(m.input.Value())
	if ok {
		if name == "agent" {
			name, _, _ = strings.Cut(args, " ")
		}
		m.list.Search.SetValue(name)
	}
}

func completeSlashCommand(input, invocation string) string {
	name, args, ok := parseSlashInvocation(input)
	if ok && name == "agent" {
		// Replace the existing agent command name, not just the /agent prefix.
		_, args, _ = parseSlashInvocation("/" + strings.TrimPrefix(args, "/"))
	}
	if ok && args != "" {
		return invocation + " " + args
	}
	return invocation + " "
}

func slashHelp() string {
	lines := []string{"Slash commands (type / to browse; Enter inserts, then runs):"}
	for _, local := range localSlashCommands {
		label := "/" + local.Name
		if local.Arguments != "" {
			label += " " + local.Arguments
		}
		description := local.Description
		if len(local.Aliases) > 0 {
			description += " (aliases: /" + strings.Join(local.Aliases, ", /") + ")"
		}
		lines = append(lines, fmt.Sprintf("%-26s %s", label, description))
	}
	lines = append(lines, "", "Client names and aliases are reserved across harnesses. Use /agent name [args] to bypass them.", "/clear and /reset start a new conversation; they do not delete saved history.", "/resume opens the session picker; /remove clears attachments, not files.", "Unknown slash commands and multiline prompts are passed to the agent.")
	return strings.Join(lines, "\n")
}

// Keep typed command text when dismissing completion. A command unknown to the
// client can still be composed and sent to the harness normally.
func (m *Model) dismissCommandPicker() {
	if _, _, ok := parseSlashInvocation(m.input.Value()); ok {
		// Cancellation preserves typed command text, not the trailing space
		// added when accepting a completion. A bare slash deletes in one key.
		m.input.SetValue(strings.TrimSuffix(completeSlashCommand(m.input.Value(), m.commandQueryInvocation()), " "))
		m.input.CursorEnd()
	}
	m.closeOverlay()
}

func (m *Model) commandQueryInvocation() string {
	query := strings.TrimPrefix(strings.TrimSpace(m.list.Search.Value()), "/")
	if m.commandAgentOnly {
		return "/agent " + query
	}
	return "/" + query
}

func (m *Model) insertSlashCommand(invocation string) {
	m.input.SetValue(completeSlashCommand(m.input.Value(), invocation))
	m.input.CursorEnd()
}
