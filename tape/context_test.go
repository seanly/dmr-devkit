package tape

import (
	"testing"
)

func TestBuildMessagesDefault(t *testing.T) {
	ctx := NewLastAnchorContext()
	entries := []TapeEntry{
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
		NewAnchorEntry("a1", nil),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "hi"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0]["content"] != "hello" || msgs[1]["content"] != "hi" {
		t.Error("message content mismatch")
	}
}

func TestBuildMessagesWithCustomSelect(t *testing.T) {
	ctx := &TapeContext{
		Select: func(entries []TapeEntry, _ *TapeContext) []map[string]any {
			var msgs []map[string]any
			for _, e := range entries {
				if e.Kind == "event" {
					msgs = append(msgs, e.Payload)
				}
			}
			return msgs
		},
	}
	entries := []TapeEntry{
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
		NewEventEntry("run", map[string]any{"status": "ok"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0]["name"] != "run" {
		t.Error("expected event payload")
	}
}

func TestBuildMessagesFiltersAnchors(t *testing.T) {
	ctx := NewLastAnchorContext()
	entries := []TapeEntry{
		NewAnchorEntry("a1", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (anchor excluded), got %d", len(msgs))
	}
}

func TestBuildMessagesSkipsToolEntries(t *testing.T) {
	ctx := NewNoAnchorContext()
	entries := []TapeEntry{
		NewToolCallEntry([]map[string]any{{"id": "call_1", "type": "function", "function": map[string]any{"name": "echo"}}}),
		NewToolResultEntry([]any{"RESULT"}),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "done"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (only message kind), got %d", len(msgs))
	}
}
