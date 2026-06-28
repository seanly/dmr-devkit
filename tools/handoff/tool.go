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

const handoffToolDescription = `Creates a focused context checkpoint (handoff) on the current tape.

It summarizes the conversation so far, writes a handoff/tool anchor plus a compact_summary entry to the tape, and future turns load context after that anchor. This is useful for marking a phase boundary without losing key facts.

Use this tool when:
- The conversation has drifted from the original topic and you want a clean phase boundary.
- The user asks to "focus on X", "start fresh on X", or "summarize and continue with X".
- You have completed a significant investigation phase and want to compact context before the next phase.

Do not call handoff repeatedly in short succession—once per phase boundary is enough.

Parameter "focus" (optional string): topic or direction to prioritize in the summary. Leave empty for a generic compact.`

// NewTool creates the built-in handoff tool backed by the given agent.
func NewTool(a Agent) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "handoff",
			Description: handoffToolDescription,
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
