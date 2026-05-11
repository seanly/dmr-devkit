package tape

import (
	"testing"
)

func TestQueryChainBuild(t *testing.T) {
	store := NewInMemoryTapeStore()
	q := NewQuery("t", store).AfterAnchor("a1").Kinds("message").Limit(5)
	if q.opts.AfterAnchor != "a1" {
		t.Error("AfterAnchor not set")
	}
	if len(q.opts.Kinds) != 1 || q.opts.Kinds[0] != "message" {
		t.Error("Kinds not set")
	}
	if q.opts.Limit != 5 {
		t.Error("Limit not set")
	}
}

func TestQueryBetweenAnchorsAndLimit(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("s", NewMessageEntry(map[string]any{"role": "user", "content": "before"}))
	_ = store.Append("s", NewAnchorEntry("a1", nil))
	_ = store.Append("s", NewMessageEntry(map[string]any{"role": "user", "content": "task 1"}))
	_ = store.Append("s", NewMessageEntry(map[string]any{"role": "assistant", "content": "answer 1"}))
	_ = store.Append("s", NewAnchorEntry("a2", nil))
	_ = store.Append("s", NewMessageEntry(map[string]any{"role": "user", "content": "task 2"}))

	entries, err := NewQuery("s", store).BetweenAnchors("a1", "a2").Kinds("message").Limit(1).All()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Payload["content"] != "task 1" {
		t.Errorf("content = %v", entries[0].Payload["content"])
	}
}

func TestQueryCombinesAnchorDateAndText(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("c", TapeEntry{Kind: "anchor", Payload: map[string]any{"name": "a1"}, Date: "2026-03-01T00:00:00+00:00"})
	_ = store.Append("c", TapeEntry{Kind: "message", Payload: map[string]any{"role": "user", "content": "old timeout"}, Date: "2026-03-01T12:00:00+00:00"})
	_ = store.Append("c", TapeEntry{Kind: "anchor", Payload: map[string]any{"name": "a2"}, Date: "2026-03-02T00:00:00+00:00"})
	_ = store.Append("c", TapeEntry{Kind: "message", Payload: map[string]any{"role": "user", "content": "new timeout"}, Meta: map[string]any{"source": "ops"}, Date: "2026-03-02T12:00:00+00:00"})
	_ = store.Append("c", TapeEntry{Kind: "message", Payload: map[string]any{"role": "user", "content": "new success"}, Meta: map[string]any{"source": "ops"}, Date: "2026-03-03T12:00:00+00:00"})

	entries, err := NewQuery("c", store).
		AfterAnchor("a2").
		BetweenDates("2026-03-02", "2026-03-02").
		Query("timeout").
		All()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Payload["content"] != "new timeout" {
		t.Errorf("expected [new timeout], got %v", entries)
	}
}

func TestQueryAllReturnsEmpty(t *testing.T) {
	store := NewInMemoryTapeStore()
	entries, err := NewQuery("empty", store).All()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0, got %d", len(entries))
	}
}

func TestQueryTextSearch(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "Database timeout on checkout"}, WithScope("db")))
	_ = store.Append("t", NewEventEntry("run", map[string]any{"status": "ok"}, WithScope("system")))

	entries, err := NewQuery("t", store).Query("timeout").All()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != "message" {
		t.Errorf("text search failed: %v", entries)
	}

	metaEntries, err := NewQuery("t", store).Query("system").All()
	if err != nil {
		t.Fatal(err)
	}
	if len(metaEntries) != 1 || metaEntries[0].Kind != "event" {
		t.Errorf("meta search failed: %v", metaEntries)
	}
}
