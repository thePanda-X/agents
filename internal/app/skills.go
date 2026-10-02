package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Skill is a local document, not proof that a harness has registered or permitted
// it. Loading is requested through the agent, never performed by executing files.
type Skill struct{ ID, Name, Description, Path, Base, Source string }
type SkillSource struct{ Directory, Label string }

func SkillSources(workspace, home, config string) []SkillSource {
	var project []string
	for directory := filepath.Clean(workspace); ; directory = filepath.Dir(directory) {
		project = append(project, directory)
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	// Farthest project ancestor first; nearest workspace definitions override it.
	var sources []SkillSource
	for _, compatibility := range []string{".claude", ".agents"} {
		if home != "" {
			sources = append(sources, SkillSource{filepath.Join(home, compatibility, "skills"), "Global " + compatibility})
		}
		for i := len(project) - 1; i >= 0; i-- {
			sources = append(sources, SkillSource{filepath.Join(project[i], compatibility, "skills"), "Project " + compatibility})
		}
	}
	if config != "" {
		sources = append(sources, SkillSource{filepath.Join(config, "opencode", "skills"), "Global .opencode"})
	}
	for i := len(project) - 1; i >= 0; i-- {
		sources = append(sources, SkillSource{filepath.Join(project[i], ".opencode", "skills"), "Project .opencode"})
	}
	return sources
}

func DiscoverSkills(ctx context.Context, sources []SkillSource) ([]Skill, error) {
	byID := make(map[string]Skill)
	visited := 0
	var warnings []error
	warn := func(err error) {
		if len(warnings) < 10 {
			warnings = append(warnings, err)
		}
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		directory, err := filepath.EvalSymlinks(source.Directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			warn(fmt.Errorf("scan skills: %w", err))
			continue
		}
		directory, err = filepath.Abs(directory)
		if err != nil {
			return nil, err
		}
		root, err := os.OpenRoot(directory)
		if err != nil {
			warn(err)
			continue
		}
		err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				warn(walkErr)
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			visited++
			if visited > 10000 {
				return fmt.Errorf("skill discovery exceeded 10000 entries")
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "node_modules" {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			flat := filepath.Dir(path) == "." && strings.HasSuffix(entry.Name(), ".md")
			if entry.Name() != "SKILL.md" && !flat {
				return nil
			}
			file, err := root.Open(path)
			if err != nil {
				warn(err)
				return nil
			}
			data, readErr := io.ReadAll(io.LimitReader(file, (128<<10)+1))
			closeErr := file.Close()
			if readErr != nil {
				warn(readErr)
				return nil
			}
			if closeErr != nil {
				warn(closeErr)
				return nil
			}
			if len(data) > 128<<10 || !utf8.Valid(data) {
				warn(fmt.Errorf("invalid or oversized skill document: %s", filepath.Join(directory, path)))
				return nil
			}
			id := strings.TrimSuffix(entry.Name(), ".md")
			if entry.Name() == "SKILL.md" && filepath.Dir(path) != "." {
				id = filepath.Base(filepath.Dir(path))
			}
			name, description, err := skillMetadata(data)
			if err != nil {
				warn(fmt.Errorf("skill %s: %w", filepath.Join(directory, path), err))
				return nil
			}
			if name == "" {
				name = id
			}
			absolute := filepath.Join(directory, path)
			byID[id] = Skill{ID: id, Name: name, Description: description, Path: absolute, Base: filepath.Dir(absolute), Source: source.Label}
			return nil
		})
		root.Close()
		if err != nil {
			return nil, err
		}
	}
	result := make([]Skill, 0, len(byID))
	for _, skill := range byID {
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, errors.Join(warnings...)
}

func skillMetadata(data []byte) (string, string, error) {
	text := strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", nil
	}
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "---" && strings.TrimSpace(lines[end]) != "..." {
		end++
	}
	if end == len(lines) {
		return "", "", fmt.Errorf("unterminated YAML frontmatter")
	}
	var metadata struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &metadata); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(metadata.Name), strings.TrimSpace(metadata.Description), nil
}
