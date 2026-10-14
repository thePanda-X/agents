package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"agents/internal/clientservices"
	"agents/internal/harness"
	"agents/internal/tui/overlays"
	"agents/internal/tui/theme"
	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) syncInteraction() {
	if m.interaction != nil {
		for _, pending := range m.state.Interactions {
			if pending.ID == m.interaction.ID {
				return
			}
		}
		m.interaction, m.form = nil, nil
		m.closeOverlay()
	}
	if len(m.state.Interactions) == 0 {
		return
	}
	i := m.state.Interactions[0]
	m.closeOverlay()
	m.interaction = i
	m.input.Blur()
	switch i.Kind {
	case "form":
		form, err := overlays.NewForm(i.Schema)
		if err != nil {
			m.state.LastError = err
			m.answer(i, harness.Decision{Action: "cancel"})
			return
		}
		m.form = form
		m.form.Message = i.Message
	case "permission":
		var items []overlays.Item
		for _, option := range i.Options {
			items = append(items, overlays.Item{ID: option.ID, Label: option.Name, Detail: option.Kind})
		}
		m.list = overlays.NewList("Permission requested · "+m.opts.Descriptor.Name, items, false)
		m.info.SetHeight(max(1, m.height-len(items)-12))
		m.info.SoftWrap = true
		m.info.SetContent(theme.Clean(m.permissionDetail(i)))
		m.info.GotoTop()
		// Preserve the agent's ordering, but focus a rejection rather than
		// making an accidental Enter authorize a tool.
		for index, option := range i.Options {
			if strings.HasPrefix(option.Kind, "reject") {
				m.list.Cursor = index
				break
			}
		}
		m.sizeInfo()
	case "url":
		m.list = overlays.NewList("External interaction · "+m.opts.Descriptor.Name, []overlays.Item{{ID: "decline", Label: "Decline"}, {ID: "accept", Label: "Open in browser"}}, false)
		host := "unknown host"
		if target, err := url.Parse(i.URL); err == nil {
			host = target.Host
		}
		m.list.Description = i.Message + "\n\nHost: " + host + "\n" + i.URL + "\n\nOpening this URL does not mean the external workflow is complete."
		m.info.SetHeight(max(1, m.height-16))
		m.info.SoftWrap = true
		m.info.SetContent(theme.Clean(m.list.Description))
		m.info.GotoTop()
		m.list.Description = ""
		m.sizeInfo()
		m.urlReviewed, m.urlOpening = m.info.AtBottom(), false
	}
}

func (m *Model) permissionDetail(i *harness.Interaction) string {
	parts := []string{i.Message}
	var input json.RawMessage
	if c := m.state.Conversations[i.SessionID]; c != nil {
		for _, entry := range c.Entries {
			if entry.Tool == nil || entry.Tool.ID != i.Tool.ID {
				continue
			}
			parts = append(parts, entry.Tool.Title)
			input = entry.Tool.Input
			parts = append(parts, entry.Tool.Locations...)
			for _, diff := range entry.Tool.Diffs {
				parts = append(parts, transcript.RenderDiff(diff))
			}
		}
	}
	if i.Tool.Title != nil {
		parts = append(parts, *i.Tool.Title)
	}
	if i.Tool.Input != nil {
		input = *i.Tool.Input
	}
	if len(input) > 0 {
		var decoded any
		if err := json.Unmarshal(input, &decoded); err == nil {
			data, _ := json.MarshalIndent(decoded, "", "  ")
			parts = append(parts, string(data))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (m *Model) interactionKey(key tea.KeyPressMsg) tea.Cmd {
	i := m.interaction
	if key.String() == "esc" {
		m.answer(i, harness.Decision{Action: "cancel"})
		return nil
	}
	if m.form != nil {
		if key.String() == "alt+s" {
			m.answer(i, harness.Decision{Action: "decline"})
			return nil
		}
		if key.String() == "ctrl+s" || key.String() == "ctrl+enter" {
			if m.form.Review {
				m.answer(i, harness.Decision{Action: "accept", Content: m.form.Content})
			} else {
				m.form.Prepare()
			}
			return nil
		}
		if key.String() == "tab" && m.form.Review {
			return m.form.Edit()
		}
		return m.form.Update(key)
	}
	if m.list == nil {
		return nil
	}
	if (i.Kind == "url" || i.Kind == "permission") && (key.String() == "pgup" || key.String() == "pgdown") {
		var cmd tea.Cmd
		m.info, cmd = m.info.Update(key)
		if i.Kind == "url" && m.info.AtBottom() {
			m.urlReviewed = true
		}
		return cmd
	}
	if key.String() == "enter" {
		item, ok := m.list.Selected()
		if !ok {
			return nil
		}
		if i.Kind == "permission" {
			m.answer(i, harness.Decision{Action: "selected", OptionID: item.ID})
			return nil
		}
		if item.ID == "decline" {
			m.answer(i, harness.Decision{Action: "decline"})
			return nil
		}
		if !m.urlReviewed || m.urlOpening {
			return nil
		}
		m.urlOpening = true
		return openURL(m.opts.Context, i)
	}
	return m.list.Update(key)
}

func (m *Model) answer(i *harness.Interaction, decision harness.Decision) {
	if i == nil {
		return
	}
	pending := m.interaction != nil && m.interaction.ID == i.ID
	for _, request := range m.state.Interactions {
		if request.ID == i.ID {
			pending = true
			break
		}
	}
	if !pending {
		return
	}
	select {
	case <-i.Done:
		decision = harness.Decision{Action: "cancel"}
	default:
	}
	select {
	case i.Reply <- decision:
	default:
	}
	m.state.Apply(harness.Event{Kind: harness.InteractionFinished, Generation: m.state.Generation, ID: i.ID})
	if m.interaction != nil && m.interaction.ID == i.ID {
		m.interaction, m.form = nil, nil
		m.closeOverlay()
	}
	m.syncInteraction()
}

func openURL(ctx context.Context, i *harness.Interaction) tea.Cmd {
	return func() tea.Msg {
		target, err := url.Parse(i.URL)
		if err != nil {
			return urlMsg{i, err}
		}
		loopback := target.Hostname() == "localhost"
		if ip := net.ParseIP(target.Hostname()); ip != nil {
			loopback = ip.IsLoopback()
		}
		if target.Host == "" || target.User != nil || (target.Scheme != "https" && !(target.Scheme == "http" && loopback)) {
			return urlMsg{i, fmt.Errorf("URL must use HTTPS (or localhost HTTP), without embedded credentials")}
		}
		if err := validateWebURL(i.URL); err != nil {
			return urlMsg{i, err}
		}
		return urlMsg{i, launchBrowser(ctx, i.URL)}
	}
}

func (m *Model) authenticate(method harness.AuthMethod) tea.Cmd {
	if c := m.state.Active(); c != nil && c.Busy {
		m.state.Notice = "Cancel the active turn before authenticating."
		return nil
	}
	if !method.Terminal {
		id, controller := method.ID, m.opts.Controller
		return command(func() { controller.Authenticate(id) })
	}
	launch := m.opts.Agent.Launch
	if launch.Command == "" {
		launch = m.opts.Descriptor.Launch
	}
	cmd := exec.Command(launch.Command, append(append([]string(nil), launch.Args...), method.Args...)...)
	cmd.Dir = m.opts.Agent.Workspace
	cmd.Env = clientservices.MergeEnv(clientservices.MergeEnv(os.Environ(), launch.Env), method.Env)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return authMsg{err} })
}

func (m *Model) externalEditor() tea.Cmd {
	editor := editorCommand()
	// This command is the user's own environment setting. The temporary path is
	// passed as a positional argument, never interpolated into shell source.
	file, err := os.CreateTemp("", "agents-draft-*.md")
	if err != nil {
		m.state.LastError = err
		return nil
	}
	path := file.Name()
	if _, err := file.WriteString(m.draftText()); err != nil {
		file.Close()
		os.Remove(path)
		m.state.LastError = err
		return nil
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		m.state.LastError = err
		return nil
	}
	cmd := exec.Command("sh", "-c", "exec "+editor+" \"$1\"", "agents-editor", path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editorMsg{err: err}
		}
		return readEditedDraft(path)
	})
}

func readEditedDraft(path string) editorMsg {
	file, err := os.Open(path)
	if err != nil {
		return editorMsg{err: err}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDraftBytes+1))
	if len(data) > maxDraftBytes {
		return editorMsg{err: fmt.Errorf("edited draft exceeds 8 MiB")}
	}
	if !utf8.Valid(data) {
		return editorMsg{err: fmt.Errorf("edited draft is not UTF-8 text")}
	}
	return editorMsg{text: theme.Clean(string(data)), err: err}
}
