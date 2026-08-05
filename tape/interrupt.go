package tape

import (
	"strings"
)

const syntheticToolInterruptResult = "Interrupted by user before this tool finished."

type interruptedExecMeta struct {
	execID         string
	suppressNotice bool
}

func collectInterruptedExecs(entries []TapeEntry) (map[string]interruptedExecMeta, map[string]bool) {
	out := make(map[string]interruptedExecMeta)
	for _, e := range entries {
		if e.Kind != "event" {
			continue
		}
		if name, _ := e.Payload["name"].(string); name != EventRunInterrupted {
			continue
		}
		data, _ := e.Payload["data"].(map[string]any)
		if data == nil {
			continue
		}
		execID, _ := data["exec_id"].(string)
		if execID == "" {
			continue
		}
		meta := interruptedExecMeta{execID: execID}
		if reason, _ := data["reason"].(string); reason == "preempted" {
			meta.suppressNotice = true
		}
		if suppress, ok := data["suppress_notice"].(bool); ok && suppress {
			meta.suppressNotice = true
		}
		out[execID] = meta
	}
	legacy := make(map[string]bool, len(out))
	for id := range out {
		legacy[id] = true
	}
	return out, legacy
}

// trimIncompleteInterruptTail keeps completed tool rounds from an interrupted exec
// and repairs orphan assistant tool_calls with synthetic tool results (P1/P3).
func trimIncompleteInterruptTail(msgs []map[string]any) []map[string]any {
	msgs = trimTrailingEmptyAssistant(msgs)
	if len(msgs) == 0 {
		return msgs
	}

	lastAssist := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		role, _ := msgs[i]["role"].(string)
		if role != "assistant" {
			continue
		}
		if _, ok := msgs[i]["tool_calls"]; ok {
			lastAssist = i
		}
		break
	}
	if lastAssist < 0 {
		return msgs
	}

	callIDs := toolCallIDsFromAssistant(msgs[lastAssist])
	if len(callIDs) == 0 {
		return trimTrailingEmptyAssistant(msgs[:lastAssist])
	}

	present := toolResultIDsAfter(msgs, lastAssist+1)
	for _, id := range callIDs {
		if present[id] {
			continue
		}
		msgs = append(msgs, map[string]any{
			"role":         "tool",
			"tool_call_id": id,
			"content":      syntheticToolInterruptResult,
		})
	}
	return msgs
}

func trimTrailingEmptyAssistant(msgs []map[string]any) []map[string]any {
	for len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		role, _ := last["role"].(string)
		if role != "assistant" {
			break
		}
		if _, hasTools := last["tool_calls"]; hasTools {
			break
		}
		content, _ := last["content"].(string)
		if strings.TrimSpace(content) != "" {
			break
		}
		msgs = msgs[:len(msgs)-1]
	}
	return msgs
}

func toolCallIDsFromAssistant(msg map[string]any) []string {
	raw, ok := msg["tool_calls"]
	if !ok || raw == nil {
		return nil
	}
	switch calls := raw.(type) {
	case []any:
		return toolCallIDsFromSlice(calls)
	case []map[string]any:
		out := make([]string, 0, len(calls))
		for _, c := range calls {
			if id, _ := c["id"].(string); id != "" {
				out = append(out, id)
			}
		}
		return out
	default:
		return nil
	}
}

func toolCallIDsFromSlice(calls []any) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func toolResultIDsAfter(msgs []map[string]any, start int) map[string]bool {
	out := make(map[string]bool)
	for i := start; i < len(msgs); i++ {
		role, _ := msgs[i]["role"].(string)
		if role != "tool" {
			break
		}
		if id, _ := msgs[i]["tool_call_id"].(string); id != "" {
			out[id] = true
		}
	}
	return out
}
