package tape

import (
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
