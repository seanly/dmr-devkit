package handoff

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/seanly/dmr-devkit/tape"
)

const taskStateExtractSystemPrompt = `You are a state tracker for an AI assistant. Your job is to read a fragment of a conversation and produce a valid TaskState v1 JSON object that captures the current task state.

TaskState v1 schema:

- goal (string, REQUIRED): The user's highest-level intent. Capture what they want to achieve, not the mechanics of tool calls. Keep it under 200 characters. Do NOT change the goal unless the user explicitly pivots to a new topic.
- constraints (object): Key user preferences, constraints, or instructions that must continue to shape behavior. Use meaningful keys such as "language", "style", "scope", "must_use", "avoid". Do NOT use generic keys like "latest" or "scope" unless there is no better choice. Only include constraints that are still active; drop stale ones.
- completed (array of objects): Work items that are clearly finished. Each item has {id, summary, step?}. Keep summaries under 120 characters. Preserve important completed items from the previous state unless the user explicitly undoes them.
- pending (array of objects): Work items that are explicitly requested but not yet done. Each item has {id, summary, depends_on?}. Preserve pending items from the previous state and add new ones introduced in the recent conversation. Mark an item done by moving it to completed, not by deleting it.
- last_action (string): The most recent tool call or significant assistant action, e.g. "fsRead(/path/to/file)" or "summarized conversation". Keep it under 120 characters.
- active_files (array of strings): File paths currently being discussed or modified. Keep the most relevant 5-10 paths. Preserve paths from the previous state that are still relevant, and add newly referenced ones.
- artifacts (array of objects): Tangible outputs produced so far. Each item has {type, ref, label?}. Examples: {"type":"file","ref":"src/main.go"}, {"type":"url","ref":"https://...","label":"docs"}. Preserve relevant artifacts from the previous state.

Update rules:
1. START from the previous TaskState provided below. Do not start from a blank slate.
2. INHERIT: Keep goal, active constraints, relevant completed/pending items, active_files, and artifacts unless the new conversation explicitly changes or supersedes them.
3. UPDATE: Add new pending items, mark completed items as done, add newly referenced files, and record the latest last_action.
4. PRUNE: Remove constraints that are no longer active. Do not delete pending items just because they were not mentioned again; only delete if they are explicitly cancelled or completed.
5. STABILITY: If the recent conversation does not meaningfully change the task state, return the previous state with only last_action and updated_at refreshed.

Output rules:
- Output ONLY valid JSON. No markdown fences, no commentary, no explanation.
- Use the exact field names above.
- Do not invent information. If a field has no value, omit it or use an empty array/object, never null for arrays/objects.
- Respond in the same language as the conversation (Chinese for Chinese conversations, English for English conversations).`

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
