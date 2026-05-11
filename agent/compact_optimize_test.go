package agent

import (
	"strings"
	"testing"
)

func TestOptimizeMessagesForSummary(t *testing.T) {
	// Test case 1: Deduplicate system prompts
	messages := []map[string]any{
		{"role": "system", "content": "You are a helpful assistant."},
		{"role": "user", "content": "Hello"},
		{"role": "system", "content": "You are a helpful assistant."}, // duplicate
		{"role": "assistant", "content": "Hi there!"},
	}

	optimized := optimizeMessagesForSummary(messages)
	// Should have: 1 system + 1 user + 1 assistant = 3 messages
	if len(optimized) != 3 {
		t.Errorf("Expected 3 messages after deduplication, got %d", len(optimized))
	}

	// Test case 2: Compress tool messages and merge consecutive messages
	messages = []map[string]any{
		{"role": "tool", "content": string(make([]byte, 1000))}, // 1000 bytes
		{"role": "user", "content": "What's the result?"},
	}

	optimized = optimizeMessagesForSummary(messages)
	// Tool message should be converted to user and merged with next user message
	if len(optimized) != 1 {
		t.Errorf("Expected 1 merged message, got %d", len(optimized))
	}
	if optimized[0]["role"] != "user" {
		t.Errorf("Expected role=user, got %v", optimized[0]["role"])
	}

	// Test case 3: Filter empty content
	messages = []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": ""}, // empty
		{"role": "user", "content": "Are you there?"},
	}

	optimized = optimizeMessagesForSummary(messages)
	// Should merge two user messages into one
	if len(optimized) != 1 {
		t.Errorf("Expected 1 merged message after filtering empty content, got %d", len(optimized))
	}

	// Test case 4: Filter old compact summaries
	messages = []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "system", "content": "[Context Summary]\nPrevious conversation summary..."},
		{"role": "assistant", "content": "Hi!"},
	}

	optimized = optimizeMessagesForSummary(messages)
	// Should filter out the compact summary message
	if len(optimized) != 2 {
		t.Errorf("Expected 2 messages after filtering compact summary, got %d", len(optimized))
	}
	for _, msg := range optimized {
		content, _ := msg["content"].(string)
		if strings.HasPrefix(content, "[Context Summary]") {
			t.Error("Compact summary should be filtered out")
		}
	}
}

func TestFilterOldCompactSummaries(t *testing.T) {
	// Test case 1: Filter single compact summary
	messages := []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "system", "content": "[Context Summary]\nPrevious summary"},
		{"role": "assistant", "content": "Hi!"},
	}

	result := filterOldCompactSummaries(messages)
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}

	// Test case 2: Filter multiple compact summaries
	messages = []map[string]any{
		{"role": "system", "content": "[Context Summary]\nFirst summary"},
		{"role": "user", "content": "Hello"},
		{"role": "system", "content": "[Context Summary]\nSecond summary"},
		{"role": "assistant", "content": "Hi!"},
		{"role": "system", "content": "[Context Summary]\nThird summary"},
	}

	result = filterOldCompactSummaries(messages)
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}

	// Test case 3: No compact summaries
	messages = []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi!"},
	}

	result = filterOldCompactSummaries(messages)
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}

	// Test case 4: Empty messages
	result = filterOldCompactSummaries([]map[string]any{})
	if len(result) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(result))
	}

	// Test case 5: Compact summary in the middle of content
	messages = []map[string]any{
		{"role": "user", "content": "Hello [Context Summary] in the middle"},
	}

	result = filterOldCompactSummaries(messages)
	// Should NOT filter - only filter if message STARTS with [Context Summary]
	if len(result) != 1 {
		t.Errorf("Expected 1 message (content in middle should not be filtered), got %d", len(result))
	}
}

func TestCalculateMessagesSize(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi there!"},
	}

	size := calculateMessagesSize(messages)
	// "user" (4) + "Hello" (5) + "assistant" (9) + "Hi there!" (9) + overhead (100)
	expectedMin := 4 + 5 + 9 + 9
	if size < expectedMin {
		t.Errorf("Expected size >= %d, got %d", expectedMin, size)
	}
}
