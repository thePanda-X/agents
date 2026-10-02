package acpclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"agents/internal/harness"

	"github.com/Tangerg/acp"
)

func optional[T any](o acp.Opt[T]) *T {
	value, ok := o.Get()
	if !ok {
		return nil
	}
	return &value
}

func value[T any](o acp.Opt[T]) T { v, _ := o.Get(); return v }

func supported[T any](o acp.Opt[T]) bool { _, ok := o.Get(); return ok }

func normalizeOptions(options acp.Opt[[]acp.SessionConfigOption], modes acp.Opt[acp.SessionModeState]) ([]harness.ConfigOption, error) {
	if configs, ok := options.Get(); ok {
		return configOptions(configs)
	}
	if state, ok := modes.Get(); ok {
		option := harness.ConfigOption{ID: "mode", Name: "Mode", Category: "mode", Type: "select", Value: string(state.CurrentModeID), Legacy: true}
		for _, mode := range state.AvailableModes {
			option.Choices = append(option.Choices, harness.Choice{Value: string(mode.ID), Name: mode.Name, Description: value(mode.Description)})
		}
		return []harness.ConfigOption{option}, nil
	}
	return nil, nil
}

func configOptions(options []acp.SessionConfigOption) ([]harness.ConfigOption, error) {
	result := make([]harness.ConfigOption, 0, len(options))
	for _, option := range options {
		normalized := harness.ConfigOption{ID: string(option.ID), Name: option.Name, Description: value(option.Description), Category: string(value(option.Category))}
		switch v := option.Value.(type) {
		case *acp.SessionConfigBoolean:
			normalized.Type, normalized.Value = "boolean", v.CurrentValue
		case *acp.SessionConfigSelect:
			normalized.Type, normalized.Value = "select", string(v.CurrentValue)
			add := func(choices []acp.SessionConfigSelectOption, group string) {
				for _, choice := range choices {
					normalized.Choices = append(normalized.Choices, harness.Choice{Value: string(choice.Value), Name: choice.Name, Description: value(choice.Description), Group: group})
				}
			}
			switch choices := v.Options.(type) {
			case *acp.SessionConfigSelectOptionsUngrouped:
				add(*choices, "")
			case *acp.SessionConfigSelectOptionsGrouped:
				for _, group := range *choices {
					add(group.Options, group.Name)
				}
			default:
				return nil, fmt.Errorf("unsupported options for %q", option.Name)
			}
		default:
			continue // Unknown option types retain the agent's default.
		}
		result = append(result, normalized)
	}
	return result, nil
}

func toolPatch(update acp.ToolCallUpdate) harness.ToolPatch {
	patch := harness.ToolPatch{ID: string(update.ToolCallID), Title: optional(update.Title), Input: optional(update.RawInput), Output: optional(update.RawOutput)}
	if kind, ok := update.Kind.Get(); ok {
		s := string(kind)
		patch.Kind = &s
	}
	if status, ok := update.Status.Get(); ok {
		s := string(status)
		patch.Status = &s
	}
	if content, ok := update.Content.Get(); ok {
		var text []string
		var diffs []harness.Diff
		var terminals []string
		for _, block := range content {
			switch v := block.(type) {
			case *acp.Content:
				text = append(text, contentText(v.Content))
			case *acp.Diff:
				diffs = append(diffs, harness.Diff{Path: v.Path, OldText: value(v.OldText), NewText: v.NewText})
			case *acp.Terminal:
				terminals = append(terminals, string(v.TerminalID))
			}
		}
		joined := strings.Join(text, "\n")
		patch.Text, patch.Diffs, patch.Terminals = &joined, &diffs, &terminals
	}
	if locations, ok := update.Locations.Get(); ok {
		paths := make([]string, 0, len(locations))
		for _, location := range locations {
			path := location.Path
			if line, ok := location.Line.Get(); ok {
				path = fmt.Sprintf("%s:%d", path, line)
			}
			paths = append(paths, path)
		}
		patch.Locations = &paths
	}
	return patch
}

func contentText(block acp.ContentBlock) string {
	switch v := block.(type) {
	case *acp.TextContent:
		return v.Text
	case *acp.ResourceLink:
		return fmt.Sprintf("[%s](%s)", v.Name, v.URI)
	case *acp.ImageContent:
		return "[Image: " + v.MimeType + "]"
	case *acp.AudioContent:
		return "[Audio: " + v.MimeType + "]"
	case *acp.EmbeddedResource:
		if text, ok := v.Resource.(*acp.TextResourceContents); ok {
			return text.Text
		}
		return "[Embedded resource]"
	default:
		return "[Unsupported content]"
	}
}

func normalizeUpdate(notification *acp.SessionNotification) (harness.Event, error) {
	e := harness.Event{SessionID: string(notification.SessionID)}
	chunk := func(c acp.ContentChunk, role string) {
		e.Kind = harness.Message
		e.Chunk = harness.MessageChunk{ID: string(value(c.MessageID)), Role: role, Text: contentText(c.Content)}
	}
	switch v := notification.Update.(type) {
	case *acp.AgentMessageChunk:
		chunk(v.ContentChunk, "assistant")
	case *acp.AgentThoughtChunk:
		chunk(v.ContentChunk, "thought")
	case *acp.UserMessageChunk:
		chunk(v.ContentChunk, "user")
	case *acp.ToolCall:
		e.Kind = harness.Tool
		e.Tool = toolPatch(acp.ToolCallUpdate{ToolCallID: v.ToolCallID, Title: acp.OptValue(v.Title), Kind: v.Kind, Status: v.Status, Content: acp.OptValue(v.Content), Locations: acp.OptValue(v.Locations), RawInput: v.RawInput, RawOutput: v.RawOutput})
	case *acp.ToolCallUpdate:
		e.Kind, e.Tool = harness.Tool, toolPatch(*v)
	case *acp.Plan:
		e.Kind = harness.Plan
		for _, entry := range v.Entries {
			e.Plan = append(e.Plan, harness.PlanEntry{Content: entry.Content, Status: string(entry.Status), Priority: string(entry.Priority)})
		}
	case *acp.AvailableCommandsUpdate:
		e.Kind = harness.Commands
		for _, command := range v.AvailableCommands {
			var hint string
			if input, ok := value(command.Input).(*acp.UnstructuredCommandInput); ok {
				hint = input.Hint
			}
			e.Commands = append(e.Commands, harness.Command{Name: command.Name, Description: command.Description, Hint: hint})
		}
	case *acp.ConfigOptionUpdate:
		e.Kind = harness.Config
		var err error
		e.Options, err = configOptions(v.ConfigOptions)
		if err != nil {
			return e, err
		}
	case *acp.CurrentModeUpdate:
		e.Kind, e.ID = harness.Mode, string(v.CurrentModeID)
	case *acp.SessionInfoUpdate:
		e.Kind, e.Text = harness.Metadata, value(v.Title)
	case *acp.UsageUpdate:
		e.Kind, e.Usage = harness.UsageChanged, harness.Usage{Used: v.Used, Size: v.Size}
		if cost, ok := v.Cost.Get(); ok {
			e.Usage.Cost = &harness.Cost{Amount: cost.Amount, Currency: cost.Currency}
		}
	default:
		return e, fmt.Errorf("unsupported session update %T", notification.Update)
	}
	return e, nil
}

func sessionInfo(info acp.SessionInfo) harness.SessionInfo {
	return harness.SessionInfo{ID: string(info.SessionID), Workspace: info.Cwd, Title: value(info.Title), UpdatedAt: value(info.UpdatedAt), AdditionalRoots: info.AdditionalDirectories}
}

func decisionResponse(decision harness.Decision) (*acp.CreateElicitationResponse, error) {
	// The SDK's generated union decoder preserves integer, array, and boolean
	// forms without an ad-hoc conversion for each schema primitive.
	data, err := json.Marshal(map[string]any{"action": decision.Action, "content": decision.Content})
	if err != nil {
		return nil, err
	}
	var response acp.CreateElicitationResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
