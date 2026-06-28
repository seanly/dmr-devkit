// Package handoff provides the built-in handoff tool for focused context compaction.
package handoff

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// Agent provides the methods needed by the handoff tool.
type Agent interface {
	CompactTapeWithFocus(ctx context.Context, tapeName, focus string) (summary string, err error)
}

// NewTool creates the built-in handoff tool backed by the given agent.
func NewTool(a Agent) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "handoff",
			Description: "Create a focused context handoff on the current tape. Summarizes the conversation so far with an optional focus and writes a compact_summary + handoff/tool anchor/event to the current tape. Use this when the conversation has drifted, a new sub-topic has emerged, or you want to explicitly mark a phase boundary while preserving key context.",
			Group:       tool.ToolGroupCore,
			AlwaysLoad:  true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"focus": map[string]any{
						"type":        "string",
						"description": "Optional topic or direction to focus the summary on. If empty, performs a normal compact.",
					},
				},
			},
		},
		Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
			return handleHandoff(a, ctx, args)
		},
	}
}

func handleHandoff(a Agent, ctx *tool.ToolContext, args map[string]any) (any, error) {
	tapeName := ctx.Tape
	if tapeName == "" {
		return nil, fmt.Errorf("tape name not available")
	}

	focus, _ := args["focus"].(string)

	// Validate that the tape manager is available so the tool fails early with a
	// clear error if invoked outside of a normal agent run.
	if tm, ok := ctx.State[tool.StateKeyTapeManager].(*tape.TapeManager); !ok || tm == nil {
		return nil, fmt.Errorf("tape manager not available")
	}

	summary, err := a.CompactTapeWithFocus(ctx.Ctx, tapeName, focus)
	if err != nil {
		return nil, fmt.Errorf("handoff failed: %w", err)
	}

	var msg string
	if focus != "" {
		msg = fmt.Sprintf("Handoff complete. Focus: %s\n\n## Summary\n\n%s", focus, summary)
	} else {
		msg = fmt.Sprintf("Handoff complete.\n\n## Summary\n\n%s", summary)
	}
	return msg, nil
}
