package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agents/internal/app"
	"agents/internal/tui/overlays"
	"agents/internal/tui/theme"
	"agents/internal/tui/transcript"

	tea "charm.land/bubbletea/v2"
)

type skillsMsg struct {
	skills     []app.Skill
	err        error
	generation uint64
}

func (m *Model) openSkills(query string) tea.Cmd {
	m.skillsGeneration++
	generation := m.skillsGeneration
	m.openList("skills", "Load a skill", nil)
	m.list.Loading = true
	m.list.Search.SetValue(query)
	m.list.Description = "Select a skill to add a loading request to your draft. Enter in the composer sends it; no scripts run during discovery."
	workspace := m.opts.Agent.Workspace
	if c := m.state.Active(); c != nil {
		workspace = c.Info.Workspace
	}
	home, _ := os.UserHomeDir()
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" && home != "" {
		config = filepath.Join(home, ".config")
	}
	sources := app.SkillSources(workspace, home, config)
	ctx := m.opts.Context
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		skills, err := app.DiscoverSkills(ctx, sources)
		return skillsMsg{skills: skills, err: err, generation: generation}
	}
}

func (m *Model) populateSkills(msg skillsMsg) {
	if m.overlay != "skills" || msg.generation != m.skillsGeneration {
		return
	}
	m.list.Loading = false
	m.list.Items = nil
	if msg.err != nil {
		m.state.LastError = msg.err
		m.list.Description = "Some skills could not be scanned: " + theme.Clean(msg.err.Error())
		if len(msg.skills) == 0 {
			m.list.Error = "Could not scan local skills."
			m.list.Hint = "Enter retry · Esc close"
			return
		}
	}
	for _, skill := range msg.skills {
		preview := fmt.Sprintf("%s\n\n%s\n\nSource: %s\nDocument: %s\nBase directory: %s\n\nEnter adds a loading request to your draft, not an immediate model call.", skill.Name, skill.Description, skill.Source, skill.Path, skill.Base)
		m.list.Items = append(m.list.Items, overlays.Item{ID: skill.ID, Label: skill.ID + " · " + theme.Clean(skill.Name), Detail: skill.Source + " · " + theme.Clean(skill.Description), Preview: preview, Value: skill})
	}
	if len(msg.skills) == 0 {
		m.list.Description = "No local skills found. Add .agents/skills/<id>/SKILL.md, .claude/skills/<id>/SKILL.md, or .opencode/skills/<id>/SKILL.md in this project or your home configuration."
	}
}

func (m *Model) stageSkill(skill app.Skill) tea.Cmd {
	request := "Load the skill with ID " + strconv.Quote(skill.ID) + " for this task. Prefer the harness's native skill loader and respect its permissions. Only if no native loader is available, read the skill document at " + strconv.Quote(skill.Path) + " and resolve supporting files relative to " + strconv.Quote(skill.Base) + ". Do not bypass a denied native skill load, and do not execute bundled scripts merely to load the skill."
	if draft := strings.TrimSpace(m.draftText()); draft != "" {
		request += "\n\n" + m.draftText()
	}
	if len(request) > maxDraftBytes || !transcript.LargeText(request) && m.input.CharLimit > 0 && len([]rune(request)) > m.input.CharLimit {
		m.state.Notice = "Draft too large to add the skill request · draft preserved"
		return m.setTranscriptFocus(false)
	}
	if !m.setDraftText(request) {
		return m.setTranscriptFocus(false)
	}
	m.state.Notice = "Skill " + theme.Clean(skill.ID) + " selected · loading request staged · Enter to send"
	return m.setTranscriptFocus(false)
}
