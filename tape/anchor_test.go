package tape

import (
	"testing"
)

func TestFindAnchorByUUID(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewAnchorEntry("a1", map[string]any{StateKeyAnchorUUID: "550e8400-e29b-41d4-a716-446655440000"}))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "hi"}))
	_ = store.Append("t", NewAnchorEntry("a2", map[string]any{StateKeyAnchorUUID: "7c9e6679-7425-40de-944b-e07fc1f90ae7"}))

	e, err := FindAnchorByUUID(store, "t", "550e8400")
	if err != nil {
		t.Fatal(err)
	}
	if got := AnchorUUIDFromEntry(e); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("uuid = %q", got)
	}

	_, err = FindAnchorByUUID(store, "t", "missing")
	if err == nil {
		t.Fatal("expected not found")
	}
}

func TestFindAnchorByUUID_AmbiguousPrefix(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewAnchorEntry("a1", map[string]any{StateKeyAnchorUUID: "550e8400-aaaa-1111-1111-111111111111"}))
	_ = store.Append("t", NewAnchorEntry("a2", map[string]any{StateKeyAnchorUUID: "550e8400-bbbb-2222-2222-222222222222"}))

	_, err := FindAnchorByUUID(store, "t", "550e8400")
	if err == nil {
		t.Fatal("expected ambiguous prefix error")
	}
}

func TestFindSubtapeByTapeUUID(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("ns", NewMessageEntry(map[string]any{"role": "user", "content": "root"}))
	_ = store.Append("ns:sub1", NewAnchorEntry("session/start", map[string]any{
		StateKeyTapeUUID: "7c9e6679-7425-40de-944b-e07fc1f90ae7",
	}))

	name, err := FindSubtapeByTapeUUID(store, "ns", "7c9e6679")
	if err != nil {
		t.Fatal(err)
	}
	if name != "ns:sub1" {
		t.Fatalf("name = %q", name)
	}

	_, err = FindSubtapeByTapeUUID(store, "ns", "missing")
	if err == nil {
		t.Fatal("expected not found")
	}
}

func TestForkAfter(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	_ = store.Append("src", NewMessageEntry(map[string]any{"role": "user", "content": "before"}))
	anchor := NewAnchorEntry("phase", map[string]any{StateKeyAnchorUUID: "550e8400-e29b-41d4-a716-446655440000", "summary": "done"})
	_ = store.Append("src", anchor)
	anchors, _ := store.FetchAll("src", &FetchOpts{Kinds: []string{"anchor"}})
	anchorID := anchors[0].ID
	_ = store.Append("src", NewMessageEntry(map[string]any{"role": "user", "content": "after"}))

	err := tc.ForkAfter("src", anchorID, "dst", map[string]any{
		StateKeyTapeUUID:   "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		"fork_anchor_uuid": "550e8400-e29b-41d4-a716-446655440000",
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := store.FetchAll("dst", nil)
	if len(entries) < 3 {
		t.Fatalf("expected session/start + after msg + fork, got %d entries", len(entries))
	}
	foundAfter := false
	for _, e := range entries {
		if e.Kind == "message" {
			if c, _ := e.Payload["content"].(string); c == "after" {
				foundAfter = true
			}
			if c, _ := e.Payload["content"].(string); c == "before" {
				t.Fatal("before message should not be copied")
			}
		}
	}
	if !foundAfter {
		t.Fatal("after message not copied")
	}
	u, _ := TapeUUIDFromSessionStart(store, "dst")
	if u != "7c9e6679-7425-40de-944b-e07fc1f90ae7" {
		t.Fatalf("tape_uuid = %q", u)
	}
}

func TestFetchOptsBeforeID(t *testing.T) {
	store := NewInMemoryTapeStore()
	for i := 0; i < 5; i++ {
		_ = store.Append("t", NewMessageEntry(map[string]any{"content": i}))
	}
	entries, err := store.FetchAll("t", &FetchOpts{AfterID: 1, BeforeID: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (id 2,3), got %d", len(entries))
	}
}
