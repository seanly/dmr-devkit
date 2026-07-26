package tape

import (
	"testing"
)

func TestInMemoryAppendEntryReturnsID(t *testing.T) {
	s := NewInMemoryTapeStore()
	id1, err := s.AppendEntry("t", NewEventEntry("a", map[string]any{"k": 1}))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	id2, err := s.AppendEntry("t", NewEventEntry("b", map[string]any{"k": 2}))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if id1 != 0 || id2 != 1 {
		t.Fatalf("ids = %d, %d; want 0, 1", id1, id2)
	}
}

func TestFetchOptsReverseAndEventName(t *testing.T) {
	s := NewInMemoryTapeStore()
	_ = s.Append("t", NewEventEntry("a", map[string]any{}))
	_ = s.Append("t", NewEventEntry("b", map[string]any{}))
	_ = s.Append("t", NewEventEntry("a", map[string]any{}))

	entries, err := s.FetchAll("t", &FetchOpts{
		Kinds:     []string{"event"},
		Reverse:   true,
		Limit:     2,
		EventName: "a",
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	// Newest first; only "a" events.
	if name, _ := entries[0].Payload["name"].(string); name != "a" {
		t.Errorf("first name = %q, want a", name)
	}
	if entries[0].ID < entries[1].ID {
		t.Error("expected newest-first order")
	}
}
