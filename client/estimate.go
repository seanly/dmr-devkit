package client

import (
	"log/slog"
	"unicode/utf8"
)

// Estimator estimates token count for messages based on content type.
// It uses different estimation strategies for Chinese and non-Chinese content
// and adds per-message overhead to account for roles, tool-call metadata, etc.
type Estimator struct {
	// bytesPerToken is used for non-Chinese content (approximately 4 chars per token)
	bytesPerToken float64
}

const (
	// messageOverhead accounts for the role token and message framing.
	messageOverhead = 4
	// toolCallOverhead accounts for id/type/function framing in an assistant tool_calls entry.
	toolCallOverhead = 6
)

// NewEstimator creates a new Estimator with default settings.
func NewEstimator() *Estimator {
	return &Estimator{
		bytesPerToken: 4.0,
	}
}

// Estimate calculates the total estimated tokens for a list of messages.
// It applies a conservative multiplier (4/3) to account for estimation errors.
func (e *Estimator) Estimate(messages []map[string]any) int {
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += e.estimateMessage(msg)
	}
	// Apply conservative multiplier (4/3) to account for estimation errors
	return int(float64(totalTokens) * 4.0 / 3.0)
}

// estimateMessage calculates estimated tokens for a single message, including
// content, role/tool-call metadata, and message framing overhead.
func (e *Estimator) estimateMessage(msg map[string]any) int {
	if msg == nil {
		return 0
	}

	role, _ := msg["role"].(string)
	// Prefer multi-modal "parts" when present to avoid double-counting "content".
	contentTokens := 0
	if parts, ok := msg["parts"]; ok {
		contentTokens = e.estimateContent(parts)
	} else {
		contentTokens = e.estimateContent(msg["content"])
	}

	var toolCallTokens int
	if tcs, ok := msg["tool_calls"].([]any); ok {
		for _, tc := range tcs {
			toolCallTokens += e.estimateToolCall(tc)
		}
	}
	// Also support the normalized []map[string]any form produced by the tape layer.
	if tcs, ok := msg["tool_calls"].([]map[string]any); ok {
		for _, tc := range tcs {
			toolCallTokens += e.estimateToolCall(tc)
		}
	}

	// Messages with no real payload cost nothing in our estimate. This keeps the
	// estimator conservative where it matters (non-empty content) while avoiding
	// counting empty filler messages.
	if contentTokens == 0 && toolCallTokens == 0 {
		return 0
	}

	tokens := messageOverhead + contentTokens + toolCallTokens
	if role != "" {
		// role name is typically one token
		tokens++
		if role == "tool" {
			if id, _ := msg["tool_call_id"].(string); id != "" {
				tokens += e.estimateString(id)
			}
		}
	}

	return tokens
}

// estimateContent returns the token estimate for a message content value.
// It handles plain strings and multi-modal parts arrays.
func (e *Estimator) estimateContent(v any) int {
	switch c := v.(type) {
	case string:
		return e.estimateString(c)
	case []any:
		var total int
		for _, part := range c {
			total += e.estimatePart(part)
		}
		return total
	case []map[string]any:
		var total int
		for _, part := range c {
			total += e.estimatePart(part)
		}
		return total
	default:
		return 0
	}
}

// estimatePart handles provider content parts (text/image_url).
func (e *Estimator) estimatePart(part any) int {
	m, ok := part.(map[string]any)
	if !ok {
		return 0
	}
	if t, _ := m["type"].(string); t == "text" {
		if text, _ := m["text"].(string); text != "" {
			return e.estimateString(text)
		}
	}
	// Image URLs have a small but non-zero token cost; account for the URL itself.
	if t, _ := m["type"].(string); t == "image_url" {
		if url, _ := m["image_url"].(string); url != "" {
			return e.estimateString(url)
		}
		if urlMap, ok := m["image_url"].(map[string]any); ok {
			if url, _ := urlMap["url"].(string); url != "" {
				return e.estimateString(url)
			}
		}
	}
	return 0
}

// estimateString estimates tokens for a string using language-aware heuristics.
func (e *Estimator) estimateString(s string) int {
	if s == "" {
		return 0
	}
	charCount := utf8.RuneCountInString(s)
	if e.isMostlyChinese(s) {
		return int(float64(charCount) / 1.5)
	}
	return int(float64(charCount) / e.bytesPerToken)
}

// estimateToolCall estimates the token cost of a single tool_call dict.
func (e *Estimator) estimateToolCall(tc any) int {
	m, ok := tc.(map[string]any)
	if !ok {
		return 0
	}
	tokens := toolCallOverhead
	if id, _ := m["id"].(string); id != "" {
		tokens += e.estimateString(id)
	}
	if fn, ok := m["function"].(map[string]any); ok {
		if name, _ := fn["name"].(string); name != "" {
			tokens += e.estimateString(name)
		}
		if args, _ := fn["arguments"].(string); args != "" {
			tokens += e.estimateString(args)
		}
	}
	return tokens
}

// isMostlyChinese checks if the content is mostly Chinese characters.
// It returns true if more than 30% of the runes are Chinese.
func (e *Estimator) isMostlyChinese(content string) bool {
	if content == "" {
		return false
	}
	chineseRunes := 0
	totalRunes := 0

	for _, r := range content {
		totalRunes++
		if r >= '一' && r <= '鿿' {
			chineseRunes++
		}
	}

	if totalRunes == 0 {
		return false
	}

	// Consider content as Chinese if more than 30% are Chinese characters
	return float64(chineseRunes)/float64(totalRunes) > 0.3
}

// truncateMessagesToLimit removes oldest non-protected messages until the
// estimated token count fits under 95% of the provided context limit.
// It always preserves the first system message and the last user message.
func truncateMessagesToLimit(messages []map[string]any, limit int) []map[string]any {
	if limit <= 0 || len(messages) == 0 {
		return messages
	}

	e := NewEstimator()
	headroom := int(float64(limit) * 0.95)
	if e.Estimate(messages) <= headroom {
		return messages
	}

	beforeCount := len(messages)
	for e.Estimate(messages) > headroom {
		protected := protectedIndices(messages)
		removed := false
		for i := 0; i < len(messages); i++ {
			if !protected[i] {
				messages = append(messages[:i], messages[i+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			break
		}
	}

	afterCount := len(messages)
	if afterCount < beforeCount {
		slog.Info("client: truncated messages to context limit", "before", beforeCount, "after", afterCount, "limit", limit)
	}
	return messages
}

// protectedIndices returns the indices that must be preserved during truncation:
// the first system message (index 0) and the last user message.
func protectedIndices(messages []map[string]any) map[int]bool {
	protected := make(map[int]bool)
	if len(messages) > 0 {
		if role, _ := messages[0]["role"].(string); role == "system" {
			protected[0] = true
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if role, _ := messages[i]["role"].(string); role == "user" {
			protected[i] = true
			break
		}
	}
	return protected
}
