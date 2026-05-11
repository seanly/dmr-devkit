package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tape"
)

// countUserTurns counts the number of user messages on the tape.
func (a *Agent) countUserTurns(tapeName string) int {
	// Limit to recent messages to avoid reading huge tapes; 1000 is enough for turn counting.
	entries, err := a.tape.Store.FetchAll(tapeName, &tape.FetchOpts{Kinds: []string{"message"}, Limit: 1000})
	if err != nil {
		return 1
	}
	turn := 0
	for _, e := range entries {
		if role, _ := e.Payload["role"].(string); role == "user" {
			turn++
		}
	}
	if turn == 0 {
		return 1
	}
	return turn
}

// resolveSystemPrompt returns the composed system prompt for a given tape
// (base prompt for tape + plugin fragments). Thread-safe: returns a new string, no shared state.
func (a *Agent) resolveSystemPrompt(ctx context.Context, tapeName string) string {
	base := a.systemPromptBaseForTape(tapeName)
	return a.hooks.ComposeSystemPrompt(ctx, base)
}

// truncateForProvider truncates s by character count (not bytes) to reduce the
// chance of hitting strict provider input-size limits.
func truncateForProvider(s string, maxChars int) string {
	if maxChars <= 0 || s == "" {
		return s
	}

	// Rune-based truncation (safer for CJK than byte-based).
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}

	// Keep both head and tail (tail often contains error summaries).
	half := maxChars / 2
	head := string(runes[:half])
	tail := string(runes[len(runes)-(maxChars-half):])

	// Add hint for model about truncation
	truncationHint := fmt.Sprintf(
		"\n... [truncated %d chars] ...\n"+
			"⚠️ Tool output was truncated due to size limit. "+
			"If you need more data, try using pagination or more specific filters.\n",
		len(runes)-maxChars,
	)

	return head + truncationHint + tail
}

// hasShellFailure checks if any shell or powershell tool result indicates a failed command.
func hasShellFailure(calls []core.ToolCallData, results []any) bool {
	for i, tr := range results {
		if i < len(calls) {
			name := calls[i].Function.Name
			if name != "shell" && name != "shellOutput" && name != "powershell" && name != "powershellOutput" {
				continue
			}
		}
		s := fmt.Sprintf("%v", tr)
		if strings.Contains(s, "❌ COMMAND FAILED") && strings.Contains(s, "exit code:") {
			return true
		}
	}
	return false
}
