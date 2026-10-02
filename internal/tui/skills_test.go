package tui

import (
	"strings"
	"testing"

	"agents/internal/app"
	"agents/internal/harness"

	tea "charm.land/bubbletea/v2"
)

func TestSkillsCommandAndAliasOpenSearchablePickerLocally(t *testing.T) {
	for _, invocation := range []string{"/skills release", "/skill release"} {
		m, adapter := modelWithCommandAdapter(t)
		m.attachments = []harness.Attachment{{Name: "keep.md"}}
		m.input.SetValue(invocation)
		cmd := m.send()
		if cmd == nil || m.overlay != "skills" || !m.list.Searchable || m.list.Search.Value() != "release" || !m.list.Loading {
			t.Fatal("skills command did not open filtered picker")
		}
		if len(adapter.prompts) != 0 || m.submitting || len(m.attachments) != 1 {
			t.Fatal("skills command sent prompt or discarded attachment")
		}
	}
}

func TestSelectingSkillStagesLoadingRequestAndPreservesDraft(t *testing.T) {
	m, adapter := modelWithCommandAdapter(t)
	m.input.SetValue("Review the authentication flow.")
	m.attachments = []harness.Attachment{{Name: "keep.md"}}
	m.openSkills("") // Do not scan the user's actual files in this test.
	skill := app.Skill{ID: "review", Name: "Code Review", Description: "Review changes", Path: "/workspace/.agents/skills/review/SKILL.md", Base: "/workspace/.agents/skills/review", Source: "Project .agents"}
	m.Update(skillsMsg{skills: []app.Skill{skill}, generation: m.skillsGeneration})
	if m.list.Loading || len(m.list.Items) != 1 {
		t.Fatal("skills not populated")
	}
	m.choose(m.list.Items[0])
	for _, expected := range []string{"Load the skill", `"review"`, skill.Path, skill.Base, "respect its permissions", "Review the authentication flow."} {
		if !strings.Contains(m.input.Value(), expected) {
			t.Fatalf("loading request missing %q", expected)
		}
	}
	if m.overlay != "" || m.focusedTranscript || !m.input.Focused() || len(m.attachments) != 1 || len(adapter.prompts) != 0 || m.submitting {
		t.Fatal("selection unexpectedly sent or lost draft state")
	}
}

func TestSkillPickerIgnoresStaleResultsAndCancelPreservesDraft(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("keep draft")
	m.openSkills("")
	old := m.skillsGeneration
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.input.Value() != "keep draft" {
		t.Fatal("cancel changed draft")
	}
	m.openSkills("")
	m.Update(skillsMsg{skills: []app.Skill{{ID: "stale"}}, generation: old})
	if len(m.list.Items) != 0 || !m.list.Loading {
		t.Fatal("stale discovery applied to reopened picker")
	}
	m.Update(skillsMsg{generation: m.skillsGeneration})
	if m.list.Loading || !strings.Contains(m.list.Description, "No local skills") {
		t.Fatal("empty skill catalog missing guidance")
	}
}

func TestNativeSkillsCommandCollisionRemainsReachable(t *testing.T) {
	m := testModel(t)
	m.state.Active().Commands = []harness.Command{{Name: "skills"}, {Name: "skill"}}
	found := 0
	for _, item := range m.slashItems(false) {
		if item.ID == "agent:skills" || item.ID == "agent:skill" {
			found++
			if !strings.HasPrefix(item.Value.(slashSelection).Invocation, "/agent ") {
				t.Fatal("native skills command shadowed without bypass")
			}
		}
	}
	if found != 2 {
		t.Fatal("native skills commands missing from picker")
	}
}

func TestOversizedSkillRequestDoesNotTruncateDraft(t *testing.T) {
	m := testModel(t)
	m.input.CharLimit = 100
	m.input.SetValue("Keep this task exactly")
	m.stageSkill(app.Skill{ID: "review", Path: "/skills/review/SKILL.md", Base: "/skills/review"})
	if m.input.Value() != "Keep this task exactly" || !strings.Contains(m.state.Notice, "draft preserved") {
		t.Fatal("skill selection truncated existing draft")
	}
}
