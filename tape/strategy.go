package tape

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/seanly/dmr-devkit/config"
)

// applyCompactStrategy applies the configured compact strategy to a message list.
// Summary is an identity transform. Custom selectors bypass this by not calling it.
func applyCompactStrategy(messages []map[string]any, strategy config.CompactStrategy) []map[string]any {
	switch {
	case strategy.IsHybrid():
		return collapseMessages(snipMessages(messages))
	case strategy.IsSnip():
		return snipMessages(messages)
	case strategy.IsCollapse():
		return collapseMessages(messages)
	case strategy.IsSemanticCollapse():
		// Semantic collapse is summarizer-only; for live context it is identity.
		return messages
	default:
		// Summary / unknown / zero value: identity.
		return messages
	}
}

// snipMessages drops empty messages and deduplicates system prompts by exact
// trimmed content. First occurrence wins; order of non-duplicate messages is
// preserved.
func snipMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return messages
	}
	seenSystem := make(map[string]bool)
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content := JoinedContent(msg)
		if strings.TrimSpace(content) == "" {
			continue
		}
		if role == "system" {
			key := strings.TrimSpace(content)
			if seenSystem[key] {
				continue
			}
			seenSystem[key] = true
		}
		out = append(out, msg)
	}
	return out
}

// collapseMessages merges adjacent messages with the same role. Tool messages are
// only merged when their tool_call_id matches, and assistant messages carrying
// tool_calls are never merged so that tool-call pairing stays intact.
func collapseMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return messages
	}
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content := JoinedContent(msg)
		if len(out) == 0 {
			out = append(out, shallowCopyMessage(msg))
			continue
		}
		last := out[len(out)-1]
		lastRole, _ := last["role"].(string)
		if !canMergeMessages(last, msg, lastRole, role) {
			out = append(out, shallowCopyMessage(msg))
			continue
		}
		lastContent := JoinedContent(last)
		merged := lastContent
		if content != "" {
			if lastContent != "" {
				merged += "\n\n---\n\n" + content
			} else {
				merged = content
			}
		}
		last["content"] = merged
	}
	return out
}

// canMergeMessages reports whether two adjacent messages can be collapsed into
// one. It requires the same role and disallows merging assistant messages that
// carry tool_calls or tool messages with differing tool_call_id values.
func canMergeMessages(last, cur map[string]any, lastRole, curRole string) bool {
	if lastRole != curRole {
		return false
	}
	if lastRole == "assistant" {
		if _, ok := last["tool_calls"]; ok {
			return false
		}
		if _, ok := cur["tool_calls"]; ok {
			return false
		}
	}
	if lastRole == "tool" {
		lastID, _ := last["tool_call_id"].(string)
		curID, _ := cur["tool_call_id"].(string)
		if lastID != curID {
			return false
		}
	}
	return true
}

// shallowCopyMessage returns a shallow copy of a message map so that mutating
// the copy does not affect the original.
func shallowCopyMessage(msg map[string]any) map[string]any {
	out := make(map[string]any, len(msg))
	for k, v := range msg {
		out[k] = v
	}
	return out
}

// SemanticCollapseMessages folds assistant tool_calls messages and their matching
// tool result messages into single user messages. It is designed for summarizer
// input only; live context must keep separate assistant/tool roles to satisfy
// OpenAI-compatible APIs.
func SemanticCollapseMessages(messages []map[string]any) []map[string]any {
	return semanticCollapseMessages(messages)
}

// semanticCollapseMessages scans messages left-to-right and collapses an
// assistant message carrying tool_calls together with immediately following
// tool messages whose tool_call_id matches one of the assistant's call IDs.
func semanticCollapseMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return messages
	}

	const toolInteractionHeader = "[Tool Interaction]"

	var out []map[string]any
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		role, _ := msg["role"].(string)
		calls, ok := extractToolCalls(msg)
		if role != "assistant" || !ok || len(calls) == 0 {
			out = append(out, msg)
			continue
		}

		// Build a lookup from call ID to call info and collect ordered IDs.
		callByID := make(map[string]map[string]any, len(calls))
		var callOrder []string
		for _, c := range calls {
			id, _ := c["id"].(string)
			if id == "" {
				continue
			}
			callByID[id] = c
			callOrder = append(callOrder, id)
		}

		// Collect matching tool results immediately following the assistant message.
		resultsByID := make(map[string][]map[string]any)
		var unmatched []map[string]any
		j := i + 1
		for ; j < len(messages); j++ {
			next := messages[j]
			nextRole, _ := next["role"].(string)
			if nextRole != "tool" {
				break
			}
			toolCallID, _ := next["tool_call_id"].(string)
			if toolCallID != "" && callByID[toolCallID] != nil {
				resultsByID[toolCallID] = append(resultsByID[toolCallID], next)
			} else {
				// Positional fallback: pair with the call at the same offset.
				idx := j - (i + 1)
				if idx < len(callOrder) {
					resultsByID[callOrder[idx]] = append(resultsByID[callOrder[idx]], next)
				} else {
					unmatched = append(unmatched, next)
				}
			}
		}

		// Build the collapsed message content.
		var b strings.Builder
		b.WriteString(toolInteractionHeader)
		for _, id := range callOrder {
			call := callByID[id]
			name, args := toolCallNameAndArgs(call)
			b.WriteString(fmt.Sprintf("\n\nTool: %s (call_id: %s)\nArguments: %s", name, id, args))
			results := resultsByID[id]
			if len(results) == 0 {
				b.WriteString("\nResult: <pending>")
				continue
			}
			for _, r := range results {
				content := JoinedContent(r)
				if content == "" {
					content = "<empty>"
				}
				b.WriteString("\nResult: ")
				b.WriteString(content)
			}
		}
		for _, r := range unmatched {
			content := JoinedContent(r)
			if content == "" {
				content = "<empty>"
			}
			b.WriteString(fmt.Sprintf("\n\n[Tool Output]\nResult: %s", content))
		}

		out = append(out, map[string]any{
			"role":    "user",
			"content": b.String(),
		})
		i = j - 1 // advance past the consumed tool messages
	}

	return out
}

// extractToolCalls returns the tool_calls slice from a message, normalizing
// []any and []map[string]any to a []map[string]any.
func extractToolCalls(msg map[string]any) ([]map[string]any, bool) {
	raw, ok := msg["tool_calls"]
	if !ok || raw == nil {
		return nil, false
	}
	switch v := raw.(type) {
	case []map[string]any:
		return v, true
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// toolCallNameAndArgs extracts the tool name and arguments string from a
// normalized tool_call map.
func toolCallNameAndArgs(call map[string]any) (string, string) {
	name := ""
	args := ""
	if fn, ok := call["function"].(map[string]any); ok {
		name, _ = fn["name"].(string)
		if argStr, ok := fn["arguments"].(string); ok {
			args = argStr
		} else if argMap, ok := fn["arguments"].(map[string]any); ok {
			raw, _ := json.Marshal(argMap)
			args = string(raw)
		} else {
			raw, _ := json.Marshal(fn["arguments"])
			args = string(raw)
		}
	}
	if name == "" {
		name = "<unknown>"
	}
	if args == "" {
		args = "{}"
	}
	return name, args
}
