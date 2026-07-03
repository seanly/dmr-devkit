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

func TestExtractLatestCompactSummaryFromEntries_Versioned(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewCompactSummaryEntryWithVersion("first summary", 1),
		tape.NewCompactSummaryEntryWithVersion("latest summary", 2),
	}

	got := extractLatestCompactSummaryFromEntries(entries)
	if got != "latest summary" {
		t.Errorf("expected 'latest summary', got %q", got)
	}
}

func TestExtractLatestCompactSummaryFromEntries_LegacyBackwardCompatible(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.TapeEntry{Kind: "compact_summary", Payload: map[string]any{"content": "legacy summary"}},
	}

	got := extractLatestCompactSummaryFromEntries(entries)
	if got != "legacy summary" {
		t.Errorf("expected 'legacy summary', got %q", got)
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

	optimized, _, _ := optimizeEntriesForSummary(entries, tape.NewLastAnchorContext(), false, 0, false)

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

func TestOptimizeEntriesForSummary_RollingSlicesAtLatestSummary(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "old"}),
		tape.NewCompactSummaryEntry("previous summary"),
		tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "new1"}),
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "new2"}),
	}

	optimized, info, _ := optimizeEntriesForSummary(entries, tape.NewLastAnchorContext(), true, 0, false)

	if !info.Enabled {
		t.Fatal("expected rolling summary to be enabled")
	}
	if info.Count != 1 {
		t.Errorf("rolling count = %d, want 1", info.Count)
	}
	if info.PreviousSummaryChars == 0 {
		t.Error("expected previous_summary_chars > 0")
	}

	// Should contain previous summary + new messages, but not "old".
	foundOld, foundNew1, foundNew2 := false, false, false
	for _, msg := range optimized {
		content, _ := msg["content"].(string)
		if strings.Contains(content, "old") {
			foundOld = true
		}
		if strings.Contains(content, "new1") {
			foundNew1 = true
		}
		if strings.Contains(content, "new2") {
			foundNew2 = true
		}
	}
	if foundOld {
		t.Error("rolling summary should not include messages before latest compact_summary")
	}
	if !foundNew1 || !foundNew2 {
		t.Errorf("rolling summary should include messages after latest compact_summary; new1=%v new2=%v", foundNew1, foundNew2)
	}
}

func TestOptimizeEntriesForSummary_RollingFallsBackWhenNoPreviousSummary(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "old"}),
		tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "new"}),
	}

	optimized, info, _ := optimizeEntriesForSummary(entries, tape.NewLastAnchorContext(), true, 0, false)

	if info.Enabled {
		t.Error("expected rolling summary to be disabled when no previous summary exists")
	}
	if len(optimized) == 0 {
		t.Fatal("expected messages")
	}
	found := false
	for _, msg := range optimized {
		if strings.Contains(JoinedContent(msg), "old") {
			found = true
			break
		}
	}
	if !found {
		t.Error("non-rolling fallback should include all messages")
	}
}

func TestOptimizeEntriesForSummary_RollingFullRefresh(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "old"}),
		tape.NewCompactSummaryEntry("previous summary"),
		tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "new"}),
	}

	optimized, info, _ := optimizeEntriesForSummary(entries, tape.NewLastAnchorContext(), true, 1, false)

	if !info.FullRefresh {
		t.Error("expected full refresh when priorCount >= fullRefresh")
	}
	if info.Enabled {
		t.Error("rolling should be disabled during full refresh")
	}
	foundOld := false
	for _, msg := range optimized {
		if strings.Contains(JoinedContent(msg), "old") {
			foundOld = true
			break
		}
	}
	if !foundOld {
		t.Error("full refresh should summarize the whole window, including older messages")
	}
}

func TestOptimizeEntriesForSummary_SemanticCollapse(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewMessageEntry(map[string]any{
			"role":    "assistant",
			"content": "",
			"tool_calls": []map[string]any{
				{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"file_path": "README.md"}`}},
			},
		}),
		tape.NewMessageEntry(map[string]any{
			"role":         "tool",
			"tool_call_id": "call_1",
			"content":      "# README\nhello",
		}),
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "thanks"}),
	}

	optimized, _, semInfo := optimizeEntriesForSummary(entries, tape.NewLastAnchorContext(), false, 0, true)

	if !semInfo.Enabled {
		t.Fatal("expected semantic collapse to be enabled")
	}
	if len(optimized) != 1 {
		t.Fatalf("expected 1 merged message after collapse + user merge, got %d", len(optimized))
	}
	content := JoinedContent(optimized[0])
	if !strings.Contains(content, "[Tool Interaction]") {
		t.Errorf("expected collapsed message to contain [Tool Interaction], got %q", content)
	}
	if !strings.Contains(content, "read_file") {
		t.Errorf("expected collapsed message to mention tool name, got %q", content)
	}
	if !strings.Contains(content, "# README") {
		t.Errorf("expected collapsed message to include result, got %q", content)
	}
	if !strings.Contains(content, "thanks") {
		t.Errorf("expected user message to be merged, got %q", content)
	}
	if optimized[0]["role"] != "user" {
		t.Errorf("expected collapsed role=user, got %v", optimized[0]["role"])
	}
}

func TestOptimizeEntriesForSummary_RollingAndSemanticCollapse(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewCompactSummaryEntry("previous summary"),
		tape.NewMessageEntry(map[string]any{
			"role":    "assistant",
			"content": "",
			"tool_calls": []map[string]any{
				{"id": "call_1", "type": "function", "function": map[string]any{"name": "search", "arguments": `{"query": "go"}`}},
			},
		}),
		tape.NewMessageEntry(map[string]any{
			"role":         "tool",
			"tool_call_id": "call_1",
			"content":      "found 3 matches",
		}),
	}

	optimized, rollingInfo, semInfo := optimizeEntriesForSummary(entries, tape.NewLastAnchorContext(), true, 0, true)

	if !rollingInfo.Enabled {
		t.Error("expected rolling summary")
	}
	if !semInfo.Enabled {
		t.Error("expected semantic collapse")
	}
	if len(optimized) != 2 {
		t.Fatalf("expected 2 messages (previous summary + tool interaction), got %d", len(optimized))
	}
	first := JoinedContent(optimized[0])
	if !strings.HasPrefix(first, previousSummaryPrefix) {
		t.Errorf("expected first message to be previous summary, got %q", first)
	}
	if !strings.Contains(JoinedContent(optimized[1]), "[Tool Interaction]") {
		t.Errorf("expected second message to be collapsed tool interaction, got %q", JoinedContent(optimized[1]))
	}
}
