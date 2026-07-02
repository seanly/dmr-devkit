package tape

import (
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
