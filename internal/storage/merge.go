package storage

import (
	"maps"
	"reflect"
	"slices"
)

// Only locally changed fields participate in a save. Concurrent edits to the
// same field use last-writer-wins; unrelated settings and drafts remain intact.
func mergeSettings(current, before, after Settings) Settings {
	mergeValue(&current.Harness, before.Harness, after.Harness)
	mergeValue(&current.Mouse, before.Mouse, after.Mouse)
	mergeValue(&current.ShowThoughts, before.ShowThoughts, after.ShowThoughts)
	mergeValue(&current.VimNavigation, before.VimNavigation, after.VimNavigation)
	mergeValue(&current.Compact, before.Compact, after.Compact)
	mergeValue(&current.Appearance, before.Appearance, after.Appearance)
	mergeValue(&current.ReducedMotion, before.ReducedMotion, after.ReducedMotion)
	mergeValue(&current.Agents, before.Agents, after.Agents)
	mergeValue(&current.MCP, before.MCP, after.MCP)
	current.Keybindings = mergeMap(current.Keybindings, before.Keybindings, after.Keybindings)
	return current
}

func mergeValue[T any](current *T, before, after T) {
	if !reflect.DeepEqual(before, after) {
		*current = after
	}
}

func mergeMap[T any](current, before, after map[string]T) map[string]T {
	if current == nil {
		current = make(map[string]T)
	}
	for key, value := range after {
		old, exists := before[key]
		if !exists || !reflect.DeepEqual(old, value) {
			current[key] = value
		}
	}
	for key := range before {
		if _, exists := after[key]; !exists {
			delete(current, key)
		}
	}
	return current
}

func mergeSnapshot(current, before, after Snapshot) Snapshot {
	for key, workspace := range after.Workspaces {
		old := before.Workspaces[key]
		merged := current.Workspaces[key]
		mergeValue(&merged.Session, old.Session, workspace.Session)
		merged.Drafts = mergeMap(merged.Drafts, old.Drafts, workspace.Drafts)
		current.Workspaces[key] = merged
	}
	for key := range before.Workspaces {
		if _, exists := after.Workspaces[key]; !exists {
			delete(current.Workspaces, key)
		}
	}
	current.Revision = max(current.Revision, after.Revision)
	return current
}

func cloneSettings(settings Settings) Settings {
	settings.Keybindings = maps.Clone(settings.Keybindings)
	settings.MCP = slices.Clone(settings.MCP)
	settings.Agents = slices.Clone(settings.Agents)
	for i := range settings.Agents {
		settings.Agents[i].Args = slices.Clone(settings.Agents[i].Args)
		settings.Agents[i].Env = maps.Clone(settings.Agents[i].Env)
	}
	return settings
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Workspaces = maps.Clone(snapshot.Workspaces)
	for key, workspace := range snapshot.Workspaces {
		workspace.Session.AdditionalRoots = slices.Clone(workspace.Session.AdditionalRoots)
		workspace.Drafts = maps.Clone(workspace.Drafts)
		snapshot.Workspaces[key] = workspace
	}
	return snapshot
}
