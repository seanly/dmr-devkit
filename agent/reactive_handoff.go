package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tape"
)

// contextOverflowInfo holds information extracted from the last round before overflow.
type contextOverflowInfo struct {
	userRequest        string
	assistantReasoning string
	toolCalls          []string
	toolResults        []string
}

// handleContextOverflow handles context overflow by compacting history and rebuilding context.
// It returns true if the overflow was handled successfully and the loop should continue.
func (a *Agent) handleContextOverflow(
	ctx context.Context,
	tapeName string,
	step int,
	originalPrompt string,
) (bool, error) {
	slog.Warn("context overflow detected", "step", step)

	handoffName := fmt.Sprintf("auto:context-overflow:%s", time.Now().UTC().Format("20060102-150405"))

	// 1. Snapshot task state and compact (or state-only fallback)
	compactOK, _ := a.performContextHandoff(ctx, tapeName, handoffName, "overflow", step)
	if !compactOK && a.handoffCfg().CompactRequired {
		return false, core.New(core.ErrKindTemporary, fmt.Sprintf("auto-compact failed at step %d", step)).
			Phase(core.PhaseCompact).With("step", step).Build()
	}

	// 2. Extract last round information
	info := a.extractLastRoundInfo(tapeName)

	// 3. Build continuation prompt
	continuationPrompt := a.buildContinuationPrompt(info)

	// 4. Restart with continuation prompt
	a.restartContextWithPrompt(ctx, tapeName, continuationPrompt)

	slog.Info("reactive handoff completed", "anchor", handoffName)
	return true, nil
}

// extractLastRoundInfo extracts information from the last round before overflow.
func (a *Agent) extractLastRoundInfo(tapeName string) *contextOverflowInfo {
	info := &contextOverflowInfo{
		toolCalls:   make([]string, 0),
		toolResults: make([]string, 0),
	}

	// Read last round from tape (before compact anchor)
	lastRoundEntries, err := a.tape.Store.FetchAll(tapeName, &tape.FetchOpts{
		Kinds: []string{"message", "tool_call", "tool_result"},
		Limit: 10, // last 10 entries
	})
	if err != nil {
		slog.Error("failed to fetch last round", "error", err)
		return info
	}

	// Extract information from entries (iterate in reverse to get most recent first)
	for i := len(lastRoundEntries) - 1; i >= 0; i-- {
		entry := lastRoundEntries[i]
		switch entry.Kind {
		case "message":
			if role, ok := entry.Payload["role"].(string); ok {
				if role == "user" && info.userRequest == "" {
					if content, ok := entry.Payload["content"].(string); ok {
						info.userRequest = content
					}
				} else if role == "assistant" && info.assistantReasoning == "" {
					if content, ok := entry.Payload["content"].(string); ok {
						info.assistantReasoning = content
					}
				}
			}
		case "tool_call":
			if calls, ok := entry.Payload["calls"].([]any); ok {
				for _, call := range calls {
					if callMap, ok := call.(map[string]any); ok {
						if fn, ok := callMap["function"].(map[string]any); ok {
							name, _ := fn["name"].(string)
							args, _ := fn["arguments"].(string)
							info.toolCalls = append(info.toolCalls, fmt.Sprintf("%s(%s)", name, args))
						}
					}
				}
			}
		case "tool_result":
			if results, ok := entry.Payload["results"].([]any); ok {
				for _, result := range results {
					resultStr := fmt.Sprintf("%v", result)
					if len(resultStr) > 500 {
						resultStr = resultStr[:500] + "... [truncated]"
					}
					info.toolResults = append(info.toolResults, resultStr)
				}
			}
		}
	}

	return info
}

// buildContinuationPrompt builds a structured continuation prompt after overflow.
func (a *Agent) buildContinuationPrompt(info *contextOverflowInfo) string {
	var builder strings.Builder

	builder.WriteString("Context overflow occurred. I have compacted the conversation history.\n\n")

	if info.userRequest != "" {
		builder.WriteString("**Your last request**:\n")
		builder.WriteString(info.userRequest)
		builder.WriteString("\n\n")
	}

	if info.assistantReasoning != "" {
		builder.WriteString("**My reasoning**:\n")
		builder.WriteString(info.assistantReasoning)
		builder.WriteString("\n\n")
	}

	if len(info.toolCalls) > 0 {
		builder.WriteString("**Tools I called**: ")
		builder.WriteString(strings.Join(info.toolCalls, ", "))
		builder.WriteString("\n\n")
	}

	if len(info.toolResults) > 0 {
		builder.WriteString("**Tool results** (truncated):\n")
		builder.WriteString(strings.Join(info.toolResults, "\n"))
		builder.WriteString("\n\n")
	}

	builder.WriteString("The tool results were too large. Please continue with the original task, ")
	builder.WriteString("but use more specific queries (e.g., pagination, filters) to avoid large results. ")
	builder.WriteString("If you need details from earlier in the conversation, use tapeSearch instead of guessing.")

	return builder.String()
}

// restartContextWithPrompt restarts the context with a new prompt after overflow.
func (a *Agent) restartContextWithPrompt(ctx context.Context, tapeName, prompt string) {
	systemPrompt := a.resolveSystemPrompt(ctx, tapeName)
	_ = a.tape.AppendEntry(tapeName, tape.NewSystemEntry(systemPrompt))
	_ = a.tape.AppendEntry(tapeName, tape.NewMessageEntry(map[string]any{
		"role":    "user",
		"content": prompt,
	}))
}

// isContextOverflowError checks if an error is a context overflow error.
// It matches both legacy RepublicError (ErrInvalidInput) and new StructuredError
// (ErrKindContextOverflow) for backward and forward compatibility.
func isContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	// Legacy path
	if core.IsErrorKind(err, core.ErrInvalidInput) {
		return true
	}
	// Structured path
	return core.IsKind(err, core.ErrKindContextOverflow)
}
