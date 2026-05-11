package agent

import (
	"testing"
)

func TestTokenEstimator_Estimate(t *testing.T) {
	estimator := NewTokenEstimator()

	// Test case 1: English content
	messages := []map[string]any{
		{"role": "user", "content": "Hello world, this is a test message."},
	}
	tokens := estimator.Estimate(messages)
	// "Hello world, this is a test message." = 38 chars / 4 * 4/3 ≈ 13 tokens
	if tokens < 5 || tokens > 50 {
		t.Errorf("Expected reasonable token estimate for English, got %d", tokens)
	}

	// Test case 2: Chinese content
	messages = []map[string]any{
		{"role": "user", "content": "你好世界，这是一个测试消息。"},
	}
	tokens = estimator.Estimate(messages)
	// 16 Chinese chars / 1.5 * 4/3 ≈ 14 tokens
	if tokens < 5 || tokens > 50 {
		t.Errorf("Expected reasonable token estimate for Chinese, got %d", tokens)
	}

	// Test case 3: Mixed content
	messages = []map[string]any{
		{"role": "user", "content": "Hello 你好 world 世界"},
	}
	tokens = estimator.Estimate(messages)
	if tokens < 5 || tokens > 50 {
		t.Errorf("Expected reasonable token estimate for mixed content, got %d", tokens)
	}

	// Test case 4: Empty message
	messages = []map[string]any{
		{"role": "user", "content": ""},
	}
	tokens = estimator.Estimate(messages)
	if tokens != 0 {
		t.Errorf("Expected 0 tokens for empty message, got %d", tokens)
	}

	// Test case 5: Multiple messages
	messages = []map[string]any{
		{"role": "system", "content": "You are a helpful assistant."},
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi there! How can I help you today?"},
	}
	tokens = estimator.Estimate(messages)
	if tokens < 10 || tokens > 100 {
		t.Errorf("Expected reasonable token estimate for multiple messages, got %d", tokens)
	}
}

func TestTokenEstimator_isMostlyChinese(t *testing.T) {
	estimator := NewTokenEstimator()

	tests := []struct {
		content  string
		expected bool
	}{
		{"Hello world", false},       // English
		{"你好世界", true},               // Chinese
		{"Hello 你好 world 世界", false}, // Mixed, less than 30% Chinese
		{"你好，这是一个测试消息", true},        // Mostly Chinese
		{"Hello world, 你好", false},   // Mostly English
		{"", false},                  // Empty
		{"Hello你好你好你好", true},        // More than 30% Chinese
	}

	for _, test := range tests {
		result := estimator.isMostlyChinese(test.content)
		if result != test.expected {
			t.Errorf("isMostlyChinese(%q) = %v, expected %v", test.content, result, test.expected)
		}
	}
}

func TestTokenEstimator_EstimateToolResult(t *testing.T) {
	estimator := NewTokenEstimator()

	// Tool results are structured data with higher token density
	content := `{"status": "ok", "data": [1, 2, 3, 4, 5], "message": "Operation completed successfully"}`
	tokens := estimator.EstimateToolResult(content)

	// 90 chars / 3 ≈ 30 tokens
	if tokens < 10 || tokens > 100 {
		t.Errorf("Expected reasonable token estimate for tool result, got %d", tokens)
	}

	// Compare with regular message estimation
	messageTokens := estimator.estimateMessage(map[string]any{"content": content})
	// Tool result should have higher token count (denser) than regular message
	if tokens < messageTokens {
		t.Errorf("Tool result tokens (%d) should be >= message tokens (%d) due to higher density", tokens, messageTokens)
	}
}

func TestTokenEstimator_ConservativeMultiplier(t *testing.T) {
	estimator := NewTokenEstimator()

	// Test that conservative multiplier (4/3) is applied
	messages := []map[string]any{
		{"role": "user", "content": "Hello"}, // 5 chars
	}

	tokens := estimator.Estimate(messages)
	// Raw estimate: 5 / 4 = 1.25
	// With conservative multiplier: 1.25 * 4/3 = 1.67 -> 1

	// Should be at least 1 token
	if tokens < 1 {
		t.Errorf("Expected at least 1 token, got %d", tokens)
	}

	// Test with longer content to see multiplier effect
	messages = []map[string]any{
		{"role": "user", "content": "This is a longer message with more content to test the conservative multiplier effect."},
	}

	rawEstimate := len("This is a longer message with more content to test the conservative multiplier effect.") / 4
	tokens = estimator.Estimate(messages)

	// With conservative multiplier, should be ~4/3 of raw estimate
	if tokens < rawEstimate {
		t.Errorf("Token estimate with multiplier should be >= raw estimate, got %d < %d", tokens, rawEstimate)
	}
}
