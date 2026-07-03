package tape

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/config"
)

func TestApplyCompactStrategySummaryIdentity(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi"},
	}
	got := applyCompactStrategy(msgs, config.CompactStrategySummary)
	if len(got) != 2 {
		t.Fatalf("summary strategy should be identity, got %d messages", len(got))
	}
}

func TestSnipDropsEmptyAndDuplicateSystem(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "same"},
		{"role": "user", "content": "hello"},
		{"role": "system", "content": "same"},
		{"role": "assistant", "content": "  "},
		{"role": "user", "content": "world"},
	}
	got := snipMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0]["content"] != "same" {
		t.Errorf("first system kept, got %v", got[0])
	}
	if got[1]["content"] != "hello" {
		t.Errorf("user message kept, got %v", got[1])
	}
	if got[2]["content"] != "world" {
		t.Errorf("second user message kept, got %v", got[2])
	}
}

func TestCollapseMergesAdjacentSameRole(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "user", "content": "world"},
		{"role": "assistant", "content": "a"},
		{"role": "assistant", "content": "b"},
	}
	got := collapseMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 collapsed messages, got %d", len(got))
	}
	if got[0]["content"] != "hello\n\n---\n\nworld" {
		t.Errorf("user content = %q", got[0]["content"])
	}
	if got[1]["content"] != "a\n\n---\n\nb" {
		t.Errorf("assistant content = %q", got[1]["content"])
	}
}

func TestCollapseDoesNotMergeToolCalls(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{map[string]any{"id": "1"}}, "content": ""},
		{"role": "assistant", "content": "extra"},
	}
	got := collapseMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages when first has tool_calls, got %d", len(got))
	}
}

func TestCollapseDoesNotMergeDifferentToolCallID(t *testing.T) {
	msgs := []map[string]any{
		{"role": "tool", "tool_call_id": "a", "content": "one"},
		{"role": "tool", "tool_call_id": "b", "content": "two"},
	}
	got := collapseMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 tool messages with different ids, got %d", len(got))
	}
}

func TestHybridSnipThenCollapse(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "same"},
		{"role": "system", "content": "same"},
		{"role": "user", "content": "hello"},
		{"role": "user", "content": ""},
		{"role": "user", "content": "world"},
	}
	got := applyCompactStrategy(msgs, config.CompactStrategyHybrid)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages (deduped system + collapsed users), got %d", len(got))
	}
	if got[0]["content"] != "same" {
		t.Errorf("system = %v", got[0])
	}
	if got[1]["content"] != "hello\n\n---\n\nworld" {
		t.Errorf("collapsed user content = %q", got[1]["content"])
	}
}

func TestSemanticCollapseForLiveContextIsIdentity(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{map[string]any{"id": "1"}}, "content": ""},
		{"role": "tool", "tool_call_id": "1", "content": "result"},
	}
	got := applyCompactStrategy(msgs, config.CompactStrategySemanticCollapse)
	if len(got) != 2 {
		t.Fatalf("expected live context to keep 2 messages, got %d", len(got))
	}
}

func TestSemanticCollapseMessages_GroupsToolCalls(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []map[string]any{
			{"id": "c1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"file_path":"a.md"}`}},
			{"id": "c2", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"file_path":"b.md"}`}},
		}},
		{"role": "tool", "tool_call_id": "c1", "content": "content-a"},
		{"role": "tool", "tool_call_id": "c2", "content": "content-b"},
		{"role": "user", "content": "thanks"},
	}
	got := SemanticCollapseMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	content, _ := got[0]["content"].(string)
	if !strings.Contains(content, "[Tool Interaction]") {
		t.Errorf("expected [Tool Interaction], got %q", content)
	}
	if !strings.Contains(content, "content-a") || !strings.Contains(content, "content-b") {
		t.Errorf("expected both results, got %q", content)
	}
	if got[0]["role"] != "user" {
		t.Errorf("expected role=user, got %v", got[0]["role"])
	}
	if got[1]["content"] != "thanks" {
		t.Errorf("expected user message preserved, got %v", got[1])
	}
}

func TestSemanticCollapseMessages_PositionalFallback(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []map[string]any{
			{"id": "c1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{}`}},
		}},
		{"role": "tool", "content": "no id"},
	}
	got := SemanticCollapseMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 collapsed message, got %d", len(got))
	}
	content, _ := got[0]["content"].(string)
	if !strings.Contains(content, "no id") {
		t.Errorf("expected positional fallback result, got %q", content)
	}
}

func TestSemanticCollapseMessages_UnmatchedToolResult(t *testing.T) {
	msgs := []map[string]any{
		{"role": "tool", "tool_call_id": "orphan", "content": "standalone"},
	}
	got := SemanticCollapseMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0]["role"] != "tool" {
		t.Errorf("expected unmatched tool message left unchanged, got %v", got[0])
	}
}
