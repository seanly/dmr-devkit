package tape

import (
	"strings"
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

func TestBuildMessagesInjectsLatestTaskState(t *testing.T) {
	ctx := NewNoAnchorContext()
	payload := map[string]any{
		"schema_version": 1,
		"goal":           "Ship feature",
		"source":         "handoff",
		"updated_at":     "2026-06-17T10:00:00Z",
	}
	entries := []TapeEntry{
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
		NewTaskStateEntry(payload),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "ok"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 3 {
		t.Fatalf("expected task_state system + 2 messages, got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" {
		t.Fatalf("first message role = %v", msgs[0]["role"])
	}
	content, _ := msgs[0]["content"].(string)
	if !strings.Contains(content, "TaskState v1") || !strings.Contains(content, "Ship feature") {
		t.Fatalf("task state block = %q", content)
	}
}

func TestBuildMessagesInjectsCompleteTaskState(t *testing.T) {
	ctx := NewNoAnchorContext()
	payload := map[string]any{
		"schema_version": 1,
		"goal":           "Refactor auth",
		"constraints":    map[string]any{"language": "Go", "style": "clean"},
		"pending": []any{
			map[string]any{"id": "p1", "summary": "Extract jwt middleware"},
		},
		"completed": []any{
			map[string]any{"id": "c1", "summary": "Add login handler"},
		},
		"last_action":  "fsRead(src/auth.go)",
		"active_files": []any{"src/auth.go", "src/middleware.go"},
		"artifacts": []any{
			map[string]any{"type": "file", "ref": "src/auth.go", "label": "auth module"},
		},
		"source":     "llm_extract",
		"updated_at": "2026-06-17T10:00:00Z",
	}
	entries := []TapeEntry{
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
		NewTaskStateEntry(payload),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected task_state system + 1 message, got %d", len(msgs))
	}
	content, _ := msgs[0]["content"].(string)
	checks := []string{
		"Refactor auth",
		"constraints:",
		"language:",
		"pending:",
		"Extract jwt middleware",
		"completed:",
		"Add login handler",
		"last_action:",
		"active_files:",
		"artifacts:",
		"auth module",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("task state block missing %q; got:\n%s", want, content)
		}
	}
}
