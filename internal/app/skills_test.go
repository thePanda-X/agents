package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverSkillsParsesMetadataAndUsesPathIDs(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "release", "SKILL.md"), "---\nname: Git Release\ndescription: >-\n  Prepare release notes\n  and tags.\n---\nInstructions")
	writeSkill(t, filepath.Join(root, "review.md"), "---\nname: 'Review Code'\ndescription: \"Check changes: carefully\"\n---\nInstructions")
	writeSkill(t, filepath.Join(root, "nested", "helper.md"), "Not a skill entry")
	writeSkill(t, filepath.Join(root, "plain", "SKILL.md"), "No frontmatter")
	skills, err := DiscoverSkills(context.Background(), []SkillSource{{root, "Project"}})
	if err != nil || len(skills) != 3 {
		t.Fatalf("discovery: %+v %v", skills, err)
	}
	if skills[0].ID != "plain" || skills[0].Name != "plain" || skills[1].ID != "release" || skills[1].Name != "Git Release" || skills[1].Description != "Prepare release notes and tags." || skills[2].ID != "review" {
		t.Fatal("incorrect metadata or IDs")
	}
	if skills[1].Base != filepath.Dir(skills[1].Path) || !filepath.IsAbs(skills[1].Path) {
		t.Fatal("skill lost base directory")
	}
}

func TestDiscoveryPrecedenceAndRepositoryBoundary(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	config := filepath.Join(home, ".config")
	repo := filepath.Join(root, "repository")
	workspace := filepath.Join(repo, "package")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(config, "opencode", "skills", "release", "SKILL.md"), "---\nname: Global\n---\n")
	writeSkill(t, filepath.Join(repo, ".opencode", "skills", "release", "SKILL.md"), "---\nname: Repository\n---\n")
	writeSkill(t, filepath.Join(workspace, ".opencode", "skills", "release", "SKILL.md"), "---\nname: Workspace\n---\n")
	writeSkill(t, filepath.Join(root, ".agents", "skills", "outside", "SKILL.md"), "Outside repository")
	writeSkill(t, filepath.Join(home, ".claude", "skills", "global", "SKILL.md"), "Global compatibility")
	skills, err := DiscoverSkills(context.Background(), SkillSources(workspace, home, config))
	if err != nil || len(skills) != 2 {
		t.Fatalf("precedence/boundary: %+v %v", skills, err)
	}
	if skills[1].ID != "release" || skills[1].Name != "Workspace" {
		t.Fatal("nearest project skill did not override global")
	}
}

func TestDiscoverySkipsBadEntriesAndSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "valid", "SKILL.md"), "Valid")
	writeSkill(t, filepath.Join(root, "broken", "SKILL.md"), "---\nname: [unfinished\n---\n")
	writeSkill(t, filepath.Join(root, "huge", "SKILL.md"), strings.Repeat("x", (128<<10)+1))
	outside := filepath.Join(t.TempDir(), "SKILL.md")
	writeSkill(t, outside, "Secret outside skill source")
	t.Run("symlink escape", func(t *testing.T) {
		if err := os.Symlink(outside, filepath.Join(root, "escape.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		skills, err := DiscoverSkills(context.Background(), []SkillSource{{root, "Project"}})
		if err == nil || len(skills) != 1 || skills[0].ID != "valid" {
			t.Fatalf("symlink escaped skill sources: %+v %v", skills, err)
		}
	})
	skills, err := DiscoverSkills(context.Background(), []SkillSource{{root, "Project"}})
	if err == nil || len(skills) != 1 || skills[0].ID != "valid" {
		t.Fatalf("bad entries hid valid skills or leaked symlink: %+v %v", skills, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverSkills(ctx, []SkillSource{{root, "Project"}}); err == nil {
		t.Fatal("discovery ignored cancellation")
	}
}
