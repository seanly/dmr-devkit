package agent

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tape"
)

func TestExtractFilePath(t *testing.T) {
	cases := []struct {
		args string
		want string
	}{
		{`{"path": "/a/b.go"}`, "/a/b.go"},
		{`{"file_path": "main.go"}`, "main.go"},
		{`{"filename": "x.txt"}`, "x.txt"},
		{`{"other": 1}`, ""},
		{`not json`, ""},
		{``, ""},
	}
	for _, c := range cases {
		got := extractFilePath(c.args)
		if got != c.want {
			t.Errorf("extractFilePath(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestRecentReadFiles(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := &Agent{tape: tm, tapeStates: newTapeStateMap(8)}

	// Seed tool_call entries with read_file calls.
	calls := []map[string]any{
		{"id": "1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path": "/old.go"}`}},
	}
	_ = store.Append("t1", tape.NewToolCallEntry(calls))
	calls2 := []map[string]any{
		{"id": "2", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path": "/new.go"}`}},
		{"id": "3", "type": "function", "function": map[string]any{"name": "bash", "arguments": `{"command": "ls"}`}},
	}
	_ = store.Append("t1", tape.NewToolCallEntry(calls2))

	files := a.recentReadFiles("t1")
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
	// Newest-first ordering.
	if files[0] != "/new.go" {
		t.Errorf("expected /new.go first, got %v", files)
	}
	if files[1] != "/old.go" {
		t.Errorf("expected /old.go second, got %v", files)
	}
}

func TestRebuildPostCompactContext_AppendsEntry(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := &Agent{tape: tm, tapeStates: newTapeStateMap(8)}

	// No meta, files, or discovered tools → nothing appended.
	a.rebuildPostCompactContext(nil, "t2", compactAnchorMeta{})
	entries, _ := store.FetchAll("t2", nil)
	if len(entries) != 0 {
		t.Fatalf("expected no entries when nothing to rebuild, got %d", len(entries))
	}

	// Add a read file and a discovered tool.
	_ = store.Append("t2", tape.NewToolCallEntry([]map[string]any{
		{"id": "1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path": "main.go"}`}},
	}))
	a.DiscoverTool("t2", "my_mcp_tool")

	meta := compactAnchorMeta{
		AnchorName:            "auto:preemptive:test",
		AnchorUUID:            "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		AnchorEntryID:         99,
		PreviousAnchorName:    "session/start",
		PreviousAnchorUUID:    "550e8400-e29b-41d4-a716-446655440000",
		PreviousAnchorEntryID: 1,
		TriggerReason:         "preemptive",
	}
	a.rebuildPostCompactContext(nil, "t2", meta)
	entries, _ = store.FetchAll("t2", nil)
	var sys []tape.TapeEntry
	for _, e := range entries {
		if e.Kind == "system" {
			sys = append(sys, e)
		}
	}
	if len(sys) != 1 {
		t.Fatalf("expected 1 rebuild system entry, got %d", len(sys))
	}
	content, _ := sys[0].Payload["content"].(string)
	if !strings.Contains(content, "main.go") || !strings.Contains(content, "my_mcp_tool") {
		t.Errorf("rebuild entry should mention file and tool, got:\n%s", content)
	}
	if !strings.Contains(content, `between_uuid="550e8400-e29b-41d4-a716-446655440000,7c9e6679-7425-40de-944b-e07fc1f90ae7"`) {
		t.Errorf("rebuild entry should include between_uuid example, got:\n%s", content)
	}
}

func TestRebuildPostCompactContext_FallbackWithoutPreviousUUID(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := &Agent{tape: tm, tapeStates: newTapeStateMap(8)}

	meta := compactAnchorMeta{
		AnchorName:            "auto:token-threshold:test",
		AnchorUUID:            "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		PreviousAnchorName:    "legacy/start",
		PreviousAnchorEntryID: 3,
	}
	a.rebuildPostCompactContext(nil, "t3", meta)
	entries, _ := store.FetchAll("t3", nil)
	if len(entries) != 1 || entries[0].Kind != "system" {
		t.Fatalf("expected one system entry, got %+v", entries)
	}
	content, _ := entries[0].Payload["content"].(string)
	if strings.Contains(content, "between_uuid=") {
		t.Errorf("should not suggest between_uuid without previous UUID, got:\n%s", content)
	}
	if !strings.Contains(content, `scope="all"`) || !strings.Contains(content, "tapeAnchors") {
		t.Errorf("expected fallback guidance, got:\n%s", content)
	}
}

func TestArchivedWindowToolNames(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := &Agent{tape: tm, tapeStates: newTapeStateMap(8)}

	_ = store.Append("t4", tape.NewAnchorEntry("start", map[string]any{"x": 1}))
	_ = store.Append("t4", tape.NewToolCallEntry([]map[string]any{
		{"id": "1", "type": "function", "function": map[string]any{"name": "tapeSearch", "arguments": `{}`}},
	}))
	compactAnchor := tape.NewAnchorEntry("compact:test", nil)
	_ = store.Append("t4", compactAnchor)
	anchors, _ := store.FetchAll("t4", &tape.FetchOpts{Kinds: []string{"anchor"}})
	prev := anchors[0]
	compactEntry := anchors[1]

	meta := compactAnchorMeta{
		PreviousAnchorEntryID: prev.ID,
		AnchorEntryID:         compactEntry.ID,
	}
	names := a.archivedWindowToolNames("t4", meta)
	if len(names) != 1 || names[0] != "tapeSearch" {
		t.Fatalf("archivedWindowToolNames = %v, want [tapeSearch]", names)
	}
}
