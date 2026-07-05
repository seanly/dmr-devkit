// Package compact provides the built-in compact tool for context compaction.
package compact

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// Agent provides the methods needed by the compact tool.
type Agent interface {
	CanCompactTool(tapeName string) bool
	CompactTape(ctx context.Context, tapeName string) (summary string, err error)
}

const compactToolDescription = `Creates a context checkpoint (compact) on the current tape.

It summarizes the conversation so far, writes a compact anchor plus a compact_summary entry to the tape, and future turns load context after that anchor. This is useful for marking a phase boundary without losing key facts.

Use this tool when:
- The conversation has drifted from the original topic and you want a clean phase boundary.
- You have completed a significant investigation phase and want to compact context before the next phase.

Do not call compact repeatedly in short succession—once per phase boundary is enough.`

// NewTool creates the built-in compact tool backed by the given agent.
func NewTool(a Agent) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "compact",
			Description: compactToolDescription,
			Group:       tool.ToolGroupCore,
			AlwaysLoad:  true,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
			return handleCompact(a, ctx, args)
		},
	}
}

func handleCompact(a Agent, ctx *tool.ToolContext, args map[string]any) (any, error) {
	tapeName := ctx.Tape
	if tapeName == "" {
		return nil, fmt.Errorf("tape name not available")
	}

	// Validate that the tape manager is available so the tool fails early with a
	// clear error if invoked outside of a normal agent run.
	if tm, ok := ctx.State[tool.StateKeyTapeManager].(*tape.TapeManager); !ok || tm == nil {
		return nil, fmt.Errorf("tape manager not available")
	}

	// Prevent the LLM from repeatedly compacting in short succession. User-initiated
	// slash commands bypass the tool layer, so this guard only affects LLM-driven calls.
	if !a.CanCompactTool(tapeName) {
		return "A compact was already performed very recently. Please continue the conversation before compacting again.", nil
	}

	summary, err := a.CompactTape(ctx.Ctx, tapeName)
	if err != nil {
		return nil, fmt.Errorf("compact failed: %w", err)
	}

	msg := fmt.Sprintf("Compact complete.\n\n## Summary\n\n%s", summary)
	return msg, nil
}
