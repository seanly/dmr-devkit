package handoff

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/seanly/dmr-devkit/tape"
)

const taskStateExtractSystemPrompt = `You extract structured TaskState v1 JSON from agent conversation excerpts.
Output ONLY valid JSON with optional fields: goal, constraints (object), completed, pending, last_action, active_files, artifacts.
Do not wrap in markdown fences.`

// TaskStateExtractSystemPrompt returns the system prompt for llm_extract updates.
func TaskStateExtractSystemPrompt() string { return taskStateExtractSystemPrompt }

// FormatRecentEntries renders recent tape lines for LLM state extraction.
func FormatRecentEntries(entries []tape.TapeEntry, max int) string {
	if max <= 0 {
		max = 15
	}
	start := 0
	if len(entries) > max {
		start = len(entries) - max
	}
	var b strings.Builder
	for _, e := range entries[start:] {
		switch e.Kind {
		case "message":
			role, _ := e.Payload["role"].(string)
			content, _ := e.Payload["content"].(string)
			fmt.Fprintf(&b, "[%s] %s\n", role, truncateRunes(content, 800))
		case "tool_call":
			if calls, ok := tape.ExtractToolCalls(e.Payload); ok {
				for _, c := range calls {
					fmt.Fprintf(&b, "[tool_call] %s(%s)\n", c.Name, truncateRunes(c.Arguments, 300))
				}
			}
		case "tool_result":
			fmt.Fprintf(&b, "[tool_result] %s\n", truncateRunes(fmt.Sprint(e.Payload["results"]), 500))
		}
	}
	return b.String()
}

// ParseStateJSON parses LLM output into State and merges onto base.
func ParseStateJSON(raw string, base State) (State, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var patch State
	if err := json.Unmarshal([]byte(raw), &patch); err != nil {
		return base, err
	}
	if patch.SchemaVersion == 0 {
		patch.SchemaVersion = SchemaVersion
	}
	out := base
	if patch.Goal != "" {
		out.Goal = patch.Goal
	}
	if len(patch.Constraints) > 0 {
		out.Constraints = patch.Constraints
	}
	if len(patch.Completed) > 0 {
		out.Completed = patch.Completed
	}
	if len(patch.Pending) > 0 {
		out.Pending = patch.Pending
	}
	if patch.LastAction != "" {
		out.LastAction = patch.LastAction
	}
	if len(patch.ActiveFiles) > 0 {
		out.ActiveFiles = patch.ActiveFiles
	}
	if len(patch.Artifacts) > 0 {
		out.Artifacts = patch.Artifacts
	}
	out.Source = base.Source
	out.UpdatedAt = base.UpdatedAt
	if out.Goal == "" {
		out.Goal = base.Goal
	}
	return out, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
