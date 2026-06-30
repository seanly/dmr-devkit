package agent

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tape"
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

	// Test case 4: Extract latest compact summary as previous context
	messages = []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "system", "content": "[Context Summary]\nPrevious conversation summary..."},
		{"role": "assistant", "content": "Hi!"},
	}

	optimized = optimizeMessagesForSummary(messages)
	// Should keep the latest summary as previous context, plus user + assistant
	if len(optimized) != 3 {
		t.Errorf("Expected 3 messages (previous context + user + assistant), got %d", len(optimized))
	}
	foundPrevious := false
	for _, msg := range optimized {
		content, _ := msg["content"].(string)
		if strings.HasPrefix(content, "[Previous Context Summary]") {
			foundPrevious = true
			if !strings.Contains(content, "Previous conversation summary...") {
				t.Errorf("Previous context should preserve compact summary content, got %s", content)
			}
		}
		if strings.HasPrefix(content, "[Context Summary]") {
			t.Error("Raw [Context Summary] should be transformed, not passed through")
		}
	}
	if !foundPrevious {
		t.Error("Latest compact summary should be preserved as previous context")
	}
}

func TestExtractLatestCompactSummary(t *testing.T) {
	// Test case 1: Extract single compact summary
	messages := []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "system", "content": "[Context Summary]\nPrevious summary"},
		{"role": "assistant", "content": "Hi!"},
	}

	result, summary := extractLatestCompactSummary(messages)
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}
	if summary != "Previous summary" {
		t.Errorf("Expected extracted summary 'Previous summary', got %q", summary)
	}

	// Test case 2: Keep only the latest of multiple compact summaries
	messages = []map[string]any{
		{"role": "system", "content": "[Context Summary]\nFirst summary"},
		{"role": "user", "content": "Hello"},
		{"role": "system", "content": "[Context Summary]\nSecond summary"},
		{"role": "assistant", "content": "Hi!"},
		{"role": "system", "content": "[Context Summary]\nThird summary"},
	}

	result, summary = extractLatestCompactSummary(messages)
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}
	if summary != "Third summary" {
		t.Errorf("Expected latest summary 'Third summary', got %q", summary)
	}

	// Test case 3: No compact summaries
	messages = []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi!"},
	}

	result, summary = extractLatestCompactSummary(messages)
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}
	if summary != "" {
		t.Errorf("Expected empty summary, got %q", summary)
	}

	// Test case 4: Empty messages
	result, summary = extractLatestCompactSummary([]map[string]any{})
	if len(result) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(result))
	}
	if summary != "" {
		t.Errorf("Expected empty summary, got %q", summary)
	}

	// Test case 5: Compact summary in the middle of content
	messages = []map[string]any{
		{"role": "user", "content": "Hello [Context Summary] in the middle"},
	}

	result, summary = extractLatestCompactSummary(messages)
	// Should NOT extract - only extract if message STARTS with [Context Summary]
	if len(result) != 1 {
		t.Errorf("Expected 1 message (content in middle should not be extracted), got %d", len(result))
	}
	if summary != "" {
		t.Errorf("Expected empty summary for inline marker, got %q", summary)
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

func TestTruncateRunes_MultiByte(t *testing.T) {
	s := "你好世界"
	if got := truncateRunes(s, 2); got != "你好" {
		t.Errorf("truncateRunes(%q, 2) = %q, want %q", s, got, "你好")
	}
	if got := truncateRunes(s, 10); got != s {
		t.Errorf("truncateRunes(%q, 10) should return original, got %q", s, got)
	}
}

func TestTruncateMessagesForSummary_PreservesPrefixAndRecent(t *testing.T) {
	// Build a long message list: previous summary + many user/assistant turns.
	var messages []map[string]any
	messages = append(messages, map[string]any{
		"role":    "user",
		"content": previousSummaryPrefix + strings.Repeat("past context ", 100),
	})
	for i := 0; i < 20; i++ {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": strings.Repeat("a", 500),
		})
		messages = append(messages, map[string]any{
			"role":    "assistant",
			"content": strings.Repeat("b", 500),
		})
	}

	// Force truncation to a budget that can fit the prefix + two recent turns
	// but not the full conversation.
	trimmed := truncateMessagesForSummary(messages, 1000)

	if len(trimmed) == 0 {
		t.Fatal("truncateMessagesForSummary returned no messages")
	}
	// First message should still be the previous context summary.
	first, _ := trimmed[0]["content"].(string)
	if !strings.HasPrefix(first, previousSummaryPrefix) {
		t.Error("truncateMessagesForSummary dropped the previous context summary")
	}
	// It should have dropped some of the oldest non-prefix turns.
	if len(trimmed) >= len(messages) {
		t.Errorf("expected truncation to reduce message count, got %d vs %d", len(trimmed), len(messages))
	}
}

func TestCompressToolOutputs_DoesNotMutateOriginal(t *testing.T) {
	msg := map[string]any{
		"role":    "user",
		"content": "[Tool Output]\n" + strings.Repeat("x", 1000),
	}
	compressed := compressToolOutputs([]map[string]any{msg}, 10)
	if len(compressed) != 1 {
		t.Fatal("expected 1 compressed message")
	}
	orig, _ := msg["content"].(string)
	if !strings.HasSuffix(orig, strings.Repeat("x", 1000)) {
		t.Error("original message was mutated")
	}
	comp, _ := compressed[0]["content"].(string)
	if strings.Contains(comp, strings.Repeat("x", 1000)) {
		t.Error("compressed message still contains full tool output")
	}
}

func TestExtractLatestCompactSummaryFromEntries(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
		tape.NewCompactSummaryEntry("first summary"),
		tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "hi"}),
		tape.NewCompactSummaryEntry("latest summary"),
	}

	got := extractLatestCompactSummaryFromEntries(entries)
	if got != "latest summary" {
		t.Errorf("expected 'latest summary', got %q", got)
	}

	got = extractLatestCompactSummaryFromEntries([]tape.TapeEntry{
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	})
	if got != "" {
		t.Errorf("expected empty summary, got %q", got)
	}
}

func TestOptimizeEntriesForSummary(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewCompactSummaryEntry("old summary"),
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "msg1"}),
		tape.NewCompactSummaryEntry("latest summary"),
		tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "msg2"}),
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "msg3"}),
	}

	optimized := optimizeEntriesForSummary(entries)

	// First message should be the re-injected previous context summary.
	if len(optimized) == 0 {
		t.Fatal("expected at least one optimized message")
	}
	first, _ := optimized[0]["content"].(string)
	if !strings.HasPrefix(first, previousSummaryPrefix) {
		t.Fatalf("expected first message to start with %q, got %q", previousSummaryPrefix, first)
	}
	if !strings.Contains(first, "latest summary") {
		t.Errorf("previous summary should contain latest summary content; got %q", first)
	}

	// The optimized stream should not contain any raw compact summaries.
	for _, msg := range optimized {
		content, _ := msg["content"].(string)
		if strings.HasPrefix(content, "[Context Summary]") {
			t.Errorf("optimized stream should not contain raw [Context Summary]; got %q", content)
		}
	}

	// The remaining conversation should be present.
	found := false
	for _, msg := range optimized[1:] {
		content, _ := msg["content"].(string)
		if strings.Contains(content, "msg3") {
			found = true
			break
		}
	}
	if !found {
		t.Error("optimized stream should contain newer conversation content")
	}
}
