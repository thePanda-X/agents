package tui

func actionGroup(id string) string {
	switch id {
	case "files", "paste", "paste_image", "pastes", "clear_attachments", "attachments":
		return "Attachments"
	case "options", "effort", "harness", "auth", "reconnect", "cancel", "commands", "skills":
		return "Agent"
	case "plan", "changes", "search", "inspect", "code", "open_file", "export", "copy", "diagnostics":
		return "Review"
	case "thoughts", "compact", "settings", "help":
		return "Interface"
	default:
		return "Conversation"
	}
}

func (m *Model) actionUnavailable(id string) string {
	c := m.state.Active()
	switch id {
	case "sessions":
		if !m.state.Peer.Capabilities.List {
			return "This agent does not advertise session listing."
		}
	case "close_session":
		if c == nil {
			return "No conversation is open."
		}
		if !m.state.Peer.Capabilities.Close {
			return "This agent does not advertise closing sessions."
		}
		if c.Busy {
			return "Cancel the active turn before closing this conversation."
		}
	case "plan", "changes", "search", "inspect", "copy", "code", "open_file", "export":
		if c == nil {
			return "Send a task or resume a conversation first."
		}
		if len(c.Entries) == 0 && id != "plan" && id != "changes" && id != "search" {
			return "This conversation has no entries yet."
		}
	case "cancel":
		if m.pendingSetup == nil && m.pendingFirstPrompt == nil && (c == nil || !c.Busy) {
			return "No turn is running."
		}
	}
	return ""
}
