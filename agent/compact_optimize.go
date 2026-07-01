package agent

import (
	"strings"

	"github.com/seanly/dmr-devkit/tape"
)

const (
	maxToolContentLength       = 500
	maxPreviousSummaryRunes    = 1500
	previousSummaryRole        = "user"
	previousSummaryPrefix      = "[Previous Context Summary]\n"
	truncatedMarker            = "\n...[truncated]"
)

// optimizeMessagesForSummary optimizes messages before sending to LLM for summarization.
// It performs the following optimizations:
// 1. Extract the most recent compact summary as "previous context" and filter older ones
// 2. Deduplicate system prompts (keep only the first one)
// 3. Merge consecutive messages into conversation blocks
// 4. Compress tool outputs (truncate to maxToolContentLength)
// 5. Filter out messages with empty content
func optimizeMessagesForSummary(messages []map[string]any) []map[string]any {
	// Extract the latest compact summary as inherited context, then drop all summaries
	// from the main stream so they are not summarized twice.
	messages, previousSummary := extractLatestCompactSummary(messages)

	var optimized []map[string]any
	systemPromptSeen := make(map[string]bool)

	var currentBlock strings.Builder
	var currentRole string

	flushBlock := func() {
		if currentBlock.Len() > 0 {
			optimized = append(optimized, map[string]any{
				"role":    currentRole,
				"content": currentBlock.String(),
			})
			currentBlock.Reset()
		}
	}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content := JoinedContent(msg)

		// Skip messages with empty content
		if content == "" {
			continue
		}

		// Deduplicate system prompts
		if role == "system" {
			if systemPromptSeen[content] {
				continue
			}
			systemPromptSeen[content] = true
			flushBlock()
			optimized = append(optimized, msg)
			continue
		}

		// Compress tool outputs
		if role == "tool" {
			content = truncateRunes(content, maxToolContentLength) + truncatedMarker
			// Convert tool message to user message with prefix
			content = "[Tool Output]\n" + content
			role = "user"
		}

		// Merge consecutive messages of the same role
		if role == currentRole {
			currentBlock.WriteString("\n\n---\n\n")
			currentBlock.WriteString(content)
		} else {
			flushBlock()
			currentRole = role
			currentBlock.WriteString(content)
		}
	}

	flushBlock()

	// Inject the previous context summary as the first message so the summarizer sees it
	// as background context but still summarizes the newer conversation as the primary input.
	if previousSummary != "" {
		previousSummary = truncateRunes(previousSummary, maxPreviousSummaryRunes) + truncatedMarker
		optimized = append([]map[string]any{{
			"role":    previousSummaryRole,
			"content": previousSummaryPrefix + previousSummary,
		}}, optimized...)
	}

	return optimized
}

// truncateRunes truncates a string to the given number of runes, returning the
// original string if it is already short enough. It is safe for multi-byte UTF-8.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// truncateMessagesForSummary reduces the optimized message list so that its
// estimated token count fits within maxTokens. It preserves the previous context
// summary (if present) and the most recent conversation turns, dropping older
// messages first and compressing tool outputs further when necessary.
func truncateMessagesForSummary(messages []map[string]any, maxTokens int) []map[string]any {
	if maxTokens <= 0 || len(messages) == 0 {
		return messages
	}

	estimator := NewTokenEstimator()
	if estimator.Estimate(messages) <= maxTokens {
		return messages
	}

	// If the first message is the inherited previous context summary, keep it.
	prefix := 0
	if len(messages) > 0 {
		if role, _ := messages[0]["role"].(string); role == previousSummaryRole {
			if content, _ := messages[0]["content"].(string); strings.HasPrefix(content, previousSummaryPrefix) {
				prefix = 1
			}
		}
	}

	// First attempt: compress tool outputs more aggressively while keeping all turns.
	compressed := compressToolOutputs(messages, 200)
	if estimator.Estimate(compressed) <= maxTokens {
		return compressed
	}

	// Second attempt: drop oldest non-prefix messages one by one, keeping at least
	// the prefix plus the two most recent messages.
	for drop := 1; len(messages)-prefix-drop >= 2; drop++ {
		trimmed := make([]map[string]any, 0, len(messages)-drop)
		trimmed = append(trimmed, messages[:prefix]...)
		trimmed = append(trimmed, messages[prefix+drop:]...)
		if estimator.Estimate(trimmed) <= maxTokens {
			return trimmed
		}
	}

	// Last resort: compress tool outputs to a very small cap and keep the suffix.
	compressed = compressToolOutputs(messages, 80)
	if estimator.Estimate(compressed) <= maxTokens {
		return compressed
	}

	// If still over budget, return the compressed version; the caller should still
	// attempt summarization because the estimator is conservative.
	return compressed
}

// compressToolOutputs returns a shallow copy of messages where tool output content
// is truncated to the given rune cap. It is safe to call on already-optimized messages.
func compressToolOutputs(messages []map[string]any, cap int) []map[string]any {
	if cap <= 0 {
		return messages
	}
	out := make([]map[string]any, len(messages))
	for i, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "user" && strings.HasPrefix(content, "[Tool Output]\n") {
			prefix := "[Tool Output]\n"
			tail := strings.TrimPrefix(content, prefix)
			tail = truncateRunes(tail, cap) + truncatedMarker
			copyMsg := shallowCopyMap(msg)
			copyMsg["content"] = prefix + tail
			out[i] = copyMsg
			continue
		}
		out[i] = msg
	}
	return out
}

// shallowCopyMap returns a new map containing the same key/value pairs.
func shallowCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// extractLatestCompactSummary filters out previous compact summaries from the message
// stream and returns the most recent one as inherited context. Earlier summaries are
// dropped to avoid compounding summary length across multiple handoffs.
func extractLatestCompactSummary(messages []map[string]any) ([]map[string]any, string) {
	var result []map[string]any
	var latest string
	for _, msg := range messages {
		content, ok := msg["content"].(string)
		if ok && strings.HasPrefix(content, "[Context Summary]") {
			// Strip the prefix and keep only the latest summary seen.
			latest = strings.TrimSpace(strings.TrimPrefix(content, "[Context Summary]"))
			continue
		}
		result = append(result, msg)
	}
	return result, latest
}

// flattenMessagesForSummary flattens all messages into a single text block
// with role labels, suitable for sending as a single user message.
func flattenMessagesForSummary(messages []map[string]any) string {
	var builder strings.Builder
	builder.WriteString("=== 对话内容 ===\n\n")

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content := JoinedContent(msg)

		if content == "" {
			continue
		}

		switch role {
		case "system":
			builder.WriteString("[System Prompt]\n")
			builder.WriteString(content)
			builder.WriteString("\n\n---\n\n")
		case "user":
			builder.WriteString("[User]\n")
			builder.WriteString(content)
			builder.WriteString("\n\n---\n\n")
		case "assistant":
			builder.WriteString("[Assistant]\n")
			builder.WriteString(content)
			builder.WriteString("\n\n---\n\n")
		case "tool":
			builder.WriteString("[Tool]\n")
			builder.WriteString(content)
			builder.WriteString("\n\n---\n\n")
		}
	}

	return builder.String()
}

// calculateMessagesSize calculates the total size of messages in bytes.
func calculateMessagesSize(messages []map[string]any) int {
	size := 0
	for _, msg := range messages {
		if content, ok := msg["content"].(string); ok {
			size += len(content)
		}
		if role, ok := msg["role"].(string); ok {
			size += len(role)
		}
		// Add approximate overhead for JSON structure
		size += 50
	}
	return size
}

// extractLatestCompactSummaryFromEntries scans tape entries for the latest
// compact_summary and returns its raw content.
func extractLatestCompactSummaryFromEntries(entries []tape.TapeEntry) (summary string) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "compact_summary" {
			if data, ok := tape.ExtractCompactSummary(entries[i].Payload); ok {
				return data.Content
			}
		}
	}
	return ""
}

// optimizeEntriesForSummary prepares tape entries for the summarizer LLM.
// It extracts the latest compact_summary, builds messages from the remaining
// entries, applies the standard message-level optimizations, and then re-injects
// the previous summary as the first user message.
func optimizeEntriesForSummary(entries []tape.TapeEntry) []map[string]any {
	previousSummary := extractLatestCompactSummaryFromEntries(entries)

	// Drop all compact_summary entries so they are not summarized twice.
	filtered := make([]tape.TapeEntry, 0, len(entries))
	for _, e := range entries {
		if e.Kind == "compact_summary" {
			continue
		}
		filtered = append(filtered, e)
	}

	messages := tape.NewLastAnchorContext().BuildMessages(filtered)
	optimized := optimizeMessagesForSummary(messages)

	// Re-inject the previous context summary as the first message.
	if previousSummary != "" {
		previousSummary = truncateRunes(previousSummary, maxPreviousSummaryRunes) + truncatedMarker
		optimized = append([]map[string]any{{
			"role":    previousSummaryRole,
			"content": previousSummaryPrefix + previousSummary,
		}}, optimized...)
	}

	return optimized
}
