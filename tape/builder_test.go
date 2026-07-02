package tape

import (
	"testing"

	"github.com/seanly/dmr-devkit/config"
)

func TestContextBuilderReadMessagesLastAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	b := NewContextBuilder(store)

	msgs, err := b.ReadMessages("test_tape", NewLastAnchorContext())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0]["content"] != "task 2" {
		t.Errorf("content = %v", msgs[0]["content"])
	}
}

func TestContextBuilderReadMessagesSoftBoundary(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	b := NewContextBuilder(store)

	ctx := NewSoftBoundaryContext(2)
	msgs, err := b.ReadMessages("test_tape", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0]["content"] != "task 2" {
		t.Errorf("first after-anchor content = %v, want task 2", msgs[0]["content"])
	}
	if msgs[1]["content"] != "task 1" {
		t.Errorf("second content = %v, want task 1", msgs[1]["content"])
	}
	if msgs[2]["content"] != "answer 1" {
		t.Errorf("third content = %v, want answer 1", msgs[2]["content"])
	}
}

func TestContextBuilderReportsMissingAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	b := NewContextBuilder(store)

	_, err := b.ReadMessages("test_tape", NewNamedAnchorContext("missing"))
	if err == nil {
		t.Fatal("expected error for missing anchor")
	}
}

func TestContextBuilderAppliesStrategy(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "assistant", "content": ""}))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "assistant", "content": "world"}))

	b := NewContextBuilder(store)
	ctx := NewNoAnchorContext()
	ctx.Strategy = config.CompactStrategySnip

	msgs, err := b.ReadMessages("t", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected snip to drop empty assistant message, got %d messages", len(msgs))
	}
	if msgs[0]["content"] != "hello" || msgs[1]["content"] != "world" {
		t.Errorf("unexpected messages: %v", msgs)
	}
}
