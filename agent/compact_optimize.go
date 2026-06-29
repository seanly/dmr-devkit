package agent

import (
	"strings"
)

// optimizeMessagesForSummary optimizes messages before sending to LLM for summarization.
// It performs the following optimizations:
// 1. Extract the most recent compact summary as "previous context" and filter older ones
// 2. Deduplicate system prompts (keep only the first one)
// 3. Merge consecutive messages into conversation blocks
// 4. Compress tool outputs (truncate to maxToolContentLength)
// 5. Filter out messages with empty content
func optimizeMessagesForSummary(messages []map[string]any) []map[string]any {
	const (
		maxToolContentLength       = 500
		maxPreviousSummaryRunes    = 1500
		previousSummaryRole        = "user"
		previousSummaryPrefix      = "[Previous Context Summary]\n"
	)

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
		content, _ := msg["content"].(string)

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
			if len(content) > maxToolContentLength {
				content = content[:maxToolContentLength] + "\n...[truncated]"
			}
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
		runes := []rune(previousSummary)
		if len(runes) > maxPreviousSummaryRunes {
			previousSummary = string(runes[:maxPreviousSummaryRunes]) + "\n...[truncated]"
		}
		optimized = append([]map[string]any{{
			"role":    previousSummaryRole,
			"content": previousSummaryPrefix + previousSummary,
		}}, optimized...)
	}

	return optimized
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
		content, _ := msg["content"].(string)

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
