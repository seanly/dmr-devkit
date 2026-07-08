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

	// No files, no discovered tools → nothing appended.
	a.rebuildPostCompactContext(nil, "t2")
	entries, _ := store.FetchAll("t2", nil)
	if len(entries) != 0 {
		t.Fatalf("expected no entries when nothing to rebuild, got %d", len(entries))
	}

	// Add a read file and a discovered tool.
	_ = store.Append("t2", tape.NewToolCallEntry([]map[string]any{
		{"id": "1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path": "main.go"}`}},
	}))
	a.DiscoverTool("t2", "my_mcp_tool")

	a.rebuildPostCompactContext(nil, "t2")
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
}
