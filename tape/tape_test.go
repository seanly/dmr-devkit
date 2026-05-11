package tape

import (
	"strings"
	"testing"
	"time"
)

func TestNewMessageEntry(t *testing.T) {
	e := NewMessageEntry(map[string]any{"role": "user", "content": "hello"})
	if e.Kind != "message" {
		t.Errorf("Kind = %q, want message", e.Kind)
	}
	if e.Payload["role"] != "user" || e.Payload["content"] != "hello" {
		t.Error("payload mismatch")
	}
}

func TestNewSystemEntry(t *testing.T) {
	e := NewSystemEntry("be helpful")
	if e.Kind != "system" {
		t.Errorf("Kind = %q", e.Kind)
	}
	if e.Payload["content"] != "be helpful" {
		t.Error("content mismatch")
	}
}

func TestNewAnchorEntry(t *testing.T) {
	e := NewAnchorEntry("step1", nil)
	if e.Kind != "anchor" {
		t.Errorf("Kind = %q", e.Kind)
	}
	if e.Payload["name"] != "step1" {
		t.Error("name mismatch")
	}
}

func TestNewToolCallEntry(t *testing.T) {
	e := NewToolCallEntry([]map[string]any{{"id": "call_1", "type": "function", "function": map[string]any{"name": "echo"}}})
	if e.Kind != "tool_call" {
		t.Errorf("Kind = %q", e.Kind)
	}
}

func TestNewToolResultEntry(t *testing.T) {
	e := NewToolResultEntry([]any{"RESULT"})
	if e.Kind != "tool_result" {
		t.Errorf("Kind = %q", e.Kind)
	}
	results, ok := e.Payload["results"].([]any)
	if !ok || len(results) != 1 || results[0] != "RESULT" {
		t.Error("results mismatch")
	}
}

func TestNewErrorEntry(t *testing.T) {
	e := NewErrorEntry("not_found", "anchor missing")
	if e.Kind != "error" {
		t.Errorf("Kind = %q", e.Kind)
	}
	if e.Payload["kind"] != "not_found" {
		t.Error("error kind mismatch")
	}
}

func TestNewEventEntry(t *testing.T) {
	e := NewEventEntry("run", map[string]any{"status": "ok"})
	if e.Kind != "event" {
		t.Errorf("Kind = %q", e.Kind)
	}
	if e.Payload["name"] != "run" {
		t.Error("name mismatch")
	}
	data, ok := e.Payload["data"].(map[string]any)
	if !ok || data["status"] != "ok" {
		t.Error("data mismatch")
	}
}

func TestEntryOptionScope(t *testing.T) {
	e := NewMessageEntry(map[string]any{"role": "user", "content": "test"}, WithScope("db"))
	if e.Meta == nil || e.Meta["scope"] != "db" {
		t.Error("scope not set in Meta")
	}
}

func TestEntryDateAutoPopulated(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	e := NewMessageEntry(map[string]any{"role": "user", "content": "test"})
	after := time.Now().UTC().Add(time.Second)

	parsed, err := time.Parse(time.RFC3339, e.Date)
	if err != nil {
		t.Fatalf("failed to parse date: %v", err)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("date %v not within expected range", parsed)
	}
}

func TestEntryWithMeta(t *testing.T) {
	meta := map[string]any{"source": "ops"}
	e := NewEventEntry("run", map[string]any{"status": "ok"}, WithMeta(meta))
	if e.Meta["source"] != "ops" {
		t.Error("meta not set")
	}
}

// Store tests

func TestInMemoryAppendAndFetchAll(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t1", NewMessageEntry(map[string]any{"role": "user", "content": "hi"}))
	_ = store.Append("t1", NewMessageEntry(map[string]any{"role": "assistant", "content": "hello"}))

	entries, err := store.FetchAll("t1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestInMemoryAutoIncrementID(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t1", NewMessageEntry(map[string]any{"role": "user", "content": "a"}))
	_ = store.Append("t1", NewMessageEntry(map[string]any{"role": "user", "content": "b"}))

	entries, _ := store.FetchAll("t1", nil)
	if entries[0].ID != 0 || entries[1].ID != 1 {
		t.Errorf("IDs = %d, %d; want 0, 1", entries[0].ID, entries[1].ID)
	}
}

func TestInMemoryListTapes(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("tape_a", NewMessageEntry(map[string]any{"role": "user", "content": "a"}))
	_ = store.Append("tape_b", NewMessageEntry(map[string]any{"role": "user", "content": "b"}))

	names := store.ListTapes()
	if len(names) != 2 {
		t.Fatalf("expected 2 tapes, got %d", len(names))
	}
}

func TestInMemoryReset(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t1", NewMessageEntry(map[string]any{"role": "user", "content": "a"}))
	store.Reset("t1")

	entries, _ := store.FetchAll("t1", nil)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after reset, got %d", len(entries))
	}
}

func TestInMemoryConcurrentAccess(t *testing.T) {
	store := NewInMemoryTapeStore()
	done := make(chan bool, 10)

	for i := range 10 {
		go func(i int) {
			_ = store.Append("t1", NewMessageEntry(map[string]any{"role": "user", "content": "msg"}))
			_, _ = store.FetchAll("t1", nil)
			done <- true
		}(i)
	}
	for range 10 {
		<-done
	}

	entries, _ := store.FetchAll("t1", nil)
	if len(entries) != 10 {
		t.Errorf("expected 10 entries, got %d", len(entries))
	}
}

func TestFetchAllLastAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "before"}))
	_ = store.Append("t", NewAnchorEntry("a1", nil))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "task 1"}))
	_ = store.Append("t", NewAnchorEntry("a2", nil))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "task 2"}))

	entries, err := store.FetchAll("t", &FetchOpts{LastAnchor: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Payload["content"] != "task 2" {
		t.Errorf("content = %v", entries[0].Payload["content"])
	}
}

func TestFetchAllAfterAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "before"}))
	_ = store.Append("t", NewAnchorEntry("a1", nil))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "task 1"}))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "assistant", "content": "answer 1"}))

	entries, err := store.FetchAll("t", &FetchOpts{AfterAnchor: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestFetchAllBetweenAnchors(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewAnchorEntry("a1", nil))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "task 1"}))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "assistant", "content": "answer 1"}))
	_ = store.Append("t", NewAnchorEntry("a2", nil))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "task 2"}))

	entries, err := store.FetchAll("t", &FetchOpts{BetweenAnchors: [2]string{"a1", "a2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestFetchAllBetweenDates(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", TapeEntry{Kind: "message", Payload: map[string]any{"role": "user", "content": "before"}, Date: "2026-03-01T08:00:00+00:00"})
	_ = store.Append("t", TapeEntry{Kind: "message", Payload: map[string]any{"role": "user", "content": "during"}, Date: "2026-03-02T09:30:00+00:00"})
	_ = store.Append("t", TapeEntry{Kind: "message", Payload: map[string]any{"role": "user", "content": "after"}, Date: "2026-03-04T18:45:00+00:00"})

	entries, err := store.FetchAll("t", &FetchOpts{StartDate: "2026-03-02", EndDate: "2026-03-03"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Payload["content"] != "during" {
		t.Errorf("expected [during], got %v", entries)
	}
}

func TestFetchAllTextQuery(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "Database timeout on checkout"}, WithScope("db")))
	_ = store.Append("t", NewEventEntry("run", map[string]any{"status": "ok"}, WithScope("system")))

	entries, err := store.FetchAll("t", &FetchOpts{TextQuery: "timeout"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != "message" {
		t.Errorf("expected 1 message, got %v", entries)
	}

	metaEntries, err := store.FetchAll("t", &FetchOpts{TextQuery: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if len(metaEntries) != 1 || metaEntries[0].Kind != "event" {
		t.Errorf("expected 1 event, got %v", metaEntries)
	}
}

func TestFetchAllKindsFilter(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "hi"}))
	_ = store.Append("t", NewAnchorEntry("a1", nil))
	_ = store.Append("t", NewEventEntry("run", map[string]any{}))

	entries, err := store.FetchAll("t", &FetchOpts{Kinds: []string{"message"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != "message" {
		t.Error("kind filter failed")
	}
}

func TestFetchAllLimit(t *testing.T) {
	store := NewInMemoryTapeStore()
	for i := range 5 {
		_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": strings.Repeat("x", i+1)}))
	}

	entries, err := store.FetchAll("t", &FetchOpts{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestFetchAllMissingAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "hi"}))

	_, err := store.FetchAll("t", &FetchOpts{AfterAnchor: "missing"})
	if err == nil {
		t.Error("expected error for missing anchor")
	}
}

func TestFetchAllNoAnchorForLastAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "hi"}))

	_, err := store.FetchAll("t", &FetchOpts{LastAnchor: true})
	if err == nil {
		t.Error("expected error when no anchor exists")
	}
}

func TestFetchAllAfterID(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "a"}))
	_ = store.Append("t", NewAnchorEntry("x", nil))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "b"}))

	entries, err := store.FetchAll("t", &FetchOpts{AfterID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after AfterID=1, got %d", len(entries))
	}
	if entries[0].Payload["content"] != "b" {
		t.Errorf("content = %v, want b", entries[0].Payload["content"])
	}
}
