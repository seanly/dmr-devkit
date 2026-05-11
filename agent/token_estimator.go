package agent

import (
	"unicode/utf8"
)

// TokenEstimator estimates token count for messages based on content type.
// It uses different estimation strategies for Chinese and non-Chinese content.
type TokenEstimator struct {
	// bytesPerToken is used for non-Chinese content (approximately 4 chars per token)
	bytesPerToken float64
}

// NewTokenEstimator creates a new TokenEstimator with default settings.
func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{
		bytesPerToken: 4.0,
	}
}

// Estimate calculates the total estimated tokens for a list of messages.
// It applies a conservative multiplier (4/3) to account for estimation errors.
func (e *TokenEstimator) Estimate(messages []map[string]any) int {
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += e.estimateMessage(msg)
	}
	// Apply conservative multiplier (4/3) to account for estimation errors
	return int(float64(totalTokens) * 4.0 / 3.0)
}

// estimateMessage calculates estimated tokens for a single message.
func (e *TokenEstimator) estimateMessage(msg map[string]any) int {
	content, ok := msg["content"].(string)
	if !ok || content == "" {
		return 0
	}

	charCount := utf8.RuneCountInString(content)

	// Use different estimation based on content type
	if e.isMostlyChinese(content) {
		// Chinese: approximately 1.5 characters per token
		return int(float64(charCount) / 1.5)
	}

	// Non-Chinese: approximately 4 characters per token
	return int(float64(charCount) / e.bytesPerToken)
}

// isMostlyChinese checks if the content is mostly Chinese characters.
// It returns true if more than 30% of the runes are Chinese.
func (e *TokenEstimator) isMostlyChinese(content string) bool {
	chineseRunes := 0
	totalRunes := 0

	for _, r := range content {
		totalRunes++
		if r >= '\u4e00' && r <= '\u9fff' {
			chineseRunes++
		}
	}

	if totalRunes == 0 {
		return false
	}

	// Consider content as Chinese if more than 30% are Chinese characters
	return float64(chineseRunes)/float64(totalRunes) > 0.3
}

// EstimateToolResult estimates tokens for tool result content.
// Tool results often contain structured data which has higher token density.
func (e *TokenEstimator) EstimateToolResult(content string) int {
	charCount := utf8.RuneCountInString(content)
	// Structured data has higher token density (approximately 3 chars per token)
	return int(float64(charCount) / 3.0)
}
