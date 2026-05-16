// Package toolsearch provides the built-in toolSearch tool for deferred tool discovery.
package toolsearch

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/seanly/dmr-devkit/tool"
)

// Discovery provides the methods needed by toolSearch to query and mutate
type Discovery interface {
	GetAllExtendedTools() []*tool.Tool
	DiscoverTool(tapeName, toolName string)
	IsToolDiscovered(tapeName, toolName string) bool
}

// NewTool creates the toolSearch tool backed by the given Discovery.
func NewTool(d Discovery) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "toolSearch",
			Description: "Search for available extended tools. Query forms: (1) 'select:ToolName1,ToolName2' — fetch these exact tools by name (comma-separated), (2) 'keyword1 keyword2' — keyword search for matching tools. Use ONLY when you need functionality beyond core tools (fs, shell, tape). Examples: web search, sending messages, specialized operations.",
			Group:       tool.ToolGroupCore,
			AlwaysLoad:  true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Query to find deferred tools. Use 'select:<tool_name>' for direct selection, or keywords to search.",
					},
				},
				"required": []string{"query"},
			},
		},
		Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
			return handleToolSearch(d, ctx, args)
		},
	}
}

func handleToolSearch(d Discovery, ctx *tool.ToolContext, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	tapeName := ctx.Tape
	if tapeName == "" {
		return nil, fmt.Errorf("tape name not available")
	}

	extendedTools := d.GetAllExtendedTools()
	if len(extendedTools) == 0 {
		return "No additional tools available. All tools are already loaded.", nil
	}

	if strings.HasPrefix(query, "select:") {
		return handleSelect(d, strings.TrimPrefix(query, "select:"), tapeName, extendedTools)
	}

	matches := tool.SearchTools(extendedTools, query)
	if len(matches) == 0 {
		return fmt.Sprintf("No tools found matching '%s'. The system does not have specialized tools for this task. Please use available core tools (shell, fs, etc.) or ask the user for guidance.", query), nil
	}

	var newMatches []*tool.Tool
	for _, t := range matches {
		if !d.IsToolDiscovered(tapeName, t.Spec.Name) {
			newMatches = append(newMatches, t)
		}
	}

	for _, t := range newMatches {
		d.DiscoverTool(tapeName, t.Spec.Name)
		slog.Info("toolsearch: discovered tool", "tool", t.Spec.Name, "tape", tapeName)
	}

	var sb strings.Builder
	if len(newMatches) > 0 {
		sb.WriteString(fmt.Sprintf("Discovered %d new tool(s):\n\n", len(newMatches)))
		for _, t := range newMatches {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Spec.Name, t.Spec.Description))
		}
		sb.WriteString("\nThese tools are now available for use.")
	} else {
		sb.WriteString(fmt.Sprintf("Found %d matching tool(s), but they were already discovered:\n\n", len(matches)))
		for _, t := range matches {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Spec.Name, t.Spec.Description))
		}
	}
	return sb.String(), nil
}

func handleSelect(d Discovery, query string, tapeName string, extendedTools []*tool.Tool) (string, error) {
	requested := strings.Split(strings.ToLower(query), ",")
	var found, missing []string

	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var matched *tool.Tool
		for _, t := range extendedTools {
			if strings.ToLower(t.Spec.Name) == name {
				matched = t
				break
			}
		}
		if matched != nil {
			if !d.IsToolDiscovered(tapeName, matched.Spec.Name) {
				d.DiscoverTool(tapeName, matched.Spec.Name)
				slog.Info("toolsearch: discovered tool via select", "tool", matched.Spec.Name, "tape", tapeName)
			}
			found = append(found, matched.Spec.Name)
		} else {
			missing = append(missing, name)
		}
	}

	var sb strings.Builder
	if len(found) > 0 {
		sb.WriteString(fmt.Sprintf("Selected %d tool(s):\n\n", len(found)))
		for _, name := range found {
			sb.WriteString(fmt.Sprintf("- %s\n", name))
		}
		sb.WriteString("\nThese tools are now available for use.")
	}
	if len(missing) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("Warning: %d tool(s) not found: %s", len(missing), strings.Join(missing, ", ")))
	}
	return sb.String(), nil
}
