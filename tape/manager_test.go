package tape

import (
	"context"
	"testing"

	"github.com/seanly/dmr-devkit/core"
)

func seedEntries(store *InMemoryTapeStore) {
	_ = store.Append("test_tape", NewMessageEntry(map[string]any{"role": "user", "content": "before"}))
	_ = store.Append("test_tape", NewAnchorEntry("a1", nil))
	_ = store.Append("test_tape", NewMessageEntry(map[string]any{"role": "user", "content": "task 1"}))
	_ = store.Append("test_tape", NewMessageEntry(map[string]any{"role": "assistant", "content": "answer 1"}))
	_ = store.Append("test_tape", NewAnchorEntry("a2", nil))
	_ = store.Append("test_tape", NewMessageEntry(map[string]any{"role": "user", "content": "task 2"}))
}

func TestReadMessagesUsesLastAnchorSlice(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	mgr := NewTapeManager(store)

	msgs, err := mgr.ReadMessages("test_tape", NewLastAnchorContext())
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

func TestReadMessagesReportsMissingAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	mgr := NewTapeManager(store)

	_, err := mgr.ReadMessages("test_tape", NewNamedAnchorContext("missing"))
	if err == nil {
		t.Fatal("expected error for missing anchor")
	}
	if !core.IsErrorKind(err, core.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAppendEntry(t *testing.T) {
	store := NewInMemoryTapeStore()
	mgr := NewTapeManager(store)

	if err := mgr.AppendEntry("t1", NewMessageEntry(map[string]any{"role": "user", "content": "hi"})); err != nil {
		t.Fatal(err)
	}
	entries, _ := store.FetchAll("t1", nil)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestHandoffCreatesAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	mgr := NewTapeManager(store)

	created, err := mgr.Handoff("ops", "incident_42", map[string]any{"owner": "tier1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(created))
	}
	if created[0].Kind != "anchor" {
		t.Errorf("first entry kind = %q", created[0].Kind)
	}
	if created[1].Kind != "event" {
		t.Errorf("second entry kind = %q", created[1].Kind)
	}

	entries, _ := store.FetchAll("ops", nil)
	if len(entries) != 2 {
		t.Fatalf("expected 2 stored entries, got %d", len(entries))
	}
}

func TestRecordChat(t *testing.T) {
	store := NewInMemoryTapeStore()
	mgr := NewTapeManager(store)

	mgr.RecordChat(RecordChatOpts{
		Tape:         "t1",
		SystemPrompt: "be helpful",
		Messages: []map[string]any{
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "hi there"},
		},
		ToolCalls: []core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"text":"hello"}`}},
		},
		ToolResults: []any{"HELLO"},
		Usage:       map[string]any{"total_tokens": 42},
	})

	entries, _ := store.FetchAll("t1", nil)
	// system + 2 messages + 1 tool_call + 1 tool_result + 1 event = 6
	if len(entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(entries))
	}

	kinds := make([]string, len(entries))
	for i, e := range entries {
		kinds[i] = e.Kind
	}

	if kinds[0] != "system" {
		t.Errorf("entry 0 kind = %q", kinds[0])
	}
	if kinds[1] != "message" || kinds[2] != "message" {
		t.Error("entries 1-2 should be messages")
	}
	if kinds[3] != "tool_call" {
		t.Errorf("entry 3 kind = %q", kinds[3])
	}
	if kinds[4] != "tool_result" {
		t.Errorf("entry 4 kind = %q", kinds[4])
	}
	if kinds[5] != "event" {
		t.Errorf("entry 5 kind = %q", kinds[5])
	}

	// Check run event
	runEvent := entries[5]
	if runEvent.Payload["name"] != "run" {
		t.Error("last event should be run")
	}
	data, _ := runEvent.Payload["data"].(map[string]any)
	if data["status"] != "ok" {
		t.Error("status should be ok")
	}
}

func TestRecordChatWithError(t *testing.T) {
	store := NewInMemoryTapeStore()
	mgr := NewTapeManager(store)

	mgr.RecordChat(RecordChatOpts{
		Tape: "t1",
		Messages: []map[string]any{
			{"role": "user", "content": "hello"},
		},
		Error: &core.ErrorPayload{Kind: core.ErrProvider, Message: "server error"},
	})

	entries, _ := store.FetchAll("t1", nil)
	// 1 message + 1 error + 1 event = 3
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[1].Kind != "error" {
		t.Errorf("entry 1 kind = %q, want error", entries[1].Kind)
	}

	runEvent := entries[2]
	data, _ := runEvent.Payload["data"].(map[string]any)
	if data["status"] != "error" {
		t.Error("status should be error when ErrorPayload is set")
	}
}

func TestCompact_WritesConfiguredSummaryVersion(t *testing.T) {
	store := NewInMemoryTapeStore()
	mgr := NewTapeManager(store)

	_ = store.Append("test_tape", NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))

	created, err := mgr.Compact(context.Background(), CompactOpts{
		Tape:           "test_tape",
		AnchorName:     "test:compact",
		SummaryVersion: 2,
		Summarizer: func(ctx context.Context, messages []map[string]any) (string, error) {
			return "test summary", nil
		},
	})
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(created))
	}
	if created[1].Kind != "compact_summary" {
		t.Fatalf("expected compact_summary entry, got %q", created[1].Kind)
	}
	if got := created[1].Payload["schema_version"]; got != 2 {
		t.Errorf("payload schema_version = %v, want 2", got)
	}
	if got := created[1].Meta["schema_version"]; got != 2 {
		t.Errorf("meta schema_version = %v, want 2", got)
	}

	// Default version when SummaryVersion is 0.
	created2, err := mgr.Compact(context.Background(), CompactOpts{
		Tape:       "test_tape",
		Summarizer: func(ctx context.Context, messages []map[string]any) (string, error) { return "v1 summary", nil },
	})
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if got := created2[1].Payload["schema_version"]; got != CompactSummarySchemaVersion {
		t.Errorf("default schema_version = %v, want %d", got, CompactSummarySchemaVersion)
	}
}
