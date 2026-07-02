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
	if strings.Contains(content, "[TaskState v1]") {
		t.Fatalf("task state block should not contain legacy prefix; got:\n%s", content)
	}
	if !strings.Contains(content, "goal: Ship feature") {
		t.Fatalf("task state block missing goal; got:\n%s", content)
	}
	if msgs[0]["context_kind"] != "task_state" {
		t.Fatalf("expected context_kind=task_state, got %v", msgs[0]["context_kind"])
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
	if msgs[0]["context_kind"] != "task_state" {
		t.Fatalf("expected context_kind=task_state, got %v", msgs[0]["context_kind"])
	}
}

func TestBuildMessagesCompactSummaryHasNoPrefixAndContextKind(t *testing.T) {
	entry := NewCompactSummaryEntry("previous summary text")
	ctx := NewNoAnchorContext()
	entries := []TapeEntry{
		entry,
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected compact_summary + message, got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" {
		t.Fatalf("compact_summary role = %v", msgs[0]["role"])
	}
	content, _ := msgs[0]["content"].(string)
	if strings.Contains(content, "[Context Summary]") {
		t.Errorf("compact_summary should not contain legacy prefix; got %q", content)
	}
	if content != "previous summary text" {
		t.Errorf("compact_summary content = %q, want %q", content, "previous summary text")
	}
	if msgs[0]["context_kind"] != "compact_summary" {
		t.Fatalf("expected context_kind=compact_summary, got %v", msgs[0]["context_kind"])
	}
	if entry.Payload["schema_version"] != CompactSummarySchemaVersion {
		t.Errorf("expected schema_version %d on created entry, got %v", CompactSummarySchemaVersion, entry.Payload["schema_version"])
	}
}

func TestBuildMessagesCompactSummaryV1Unchanged(t *testing.T) {
	ctx := NewNoAnchorContext()
	entries := []TapeEntry{
		NewCompactSummaryEntryWithVersion("versioned summary", 1),
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected compact_summary + message, got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" {
		t.Fatalf("compact_summary role = %v", msgs[0]["role"])
	}
	content, _ := msgs[0]["content"].(string)
	if strings.Contains(content, "[Context Summary]") {
		t.Errorf("versioned compact_summary should not contain legacy prefix; got %q", content)
	}
	if content != "versioned summary" {
		t.Errorf("compact_summary content = %q, want %q", content, "versioned summary")
	}
	if msgs[0]["context_kind"] != "compact_summary" {
		t.Fatalf("expected context_kind=compact_summary, got %v", msgs[0]["context_kind"])
	}
}

func TestBuildMessagesCompactSummaryLegacyBackwardCompatible(t *testing.T) {
	ctx := NewNoAnchorContext()
	entries := []TapeEntry{
		{Kind: "compact_summary", Payload: map[string]any{"content": "legacy summary"}},
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected compact_summary + message, got %d", len(msgs))
	}
	content, _ := msgs[0]["content"].(string)
	if content != "legacy summary" {
		t.Errorf("legacy compact_summary content = %q, want %q", content, "legacy summary")
	}
	if msgs[0]["context_kind"] != "compact_summary" {
		t.Fatalf("expected context_kind=compact_summary, got %v", msgs[0]["context_kind"])
	}
}

func TestBuildMessagesIgnoresRuntimeSystemPrompt(t *testing.T) {
	ctx := NewNoAnchorContext()
	entries := []TapeEntry{
		NewRuntimeSystemEntry("composed runtime system prompt"),
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (runtime system_prompt ignored), got %d", len(msgs))
	}
	if msgs[0]["content"] != "hello" {
		t.Errorf("content = %q, want hello", msgs[0]["content"])
	}
}

func TestBuildMessagesSkipPoorSummaries(t *testing.T) {
	ctx := NewNoAnchorContext()
	ctx.SkipPoorSummaries = true
	entries := []TapeEntry{
		NewCompactSummaryEntryWithSourceAndQuality("good summary", CompactSummarySchemaVersion, "a1", "good"),
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
		NewCompactSummaryEntryWithSourceAndQuality("poor summary", CompactSummarySchemaVersion, "a2", "poor"),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "ok"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 3 {
		t.Fatalf("expected good summary + 2 messages, got %d", len(msgs))
	}
	if msgs[0]["content"] != "good summary" {
		t.Errorf("good summary should be kept, got %q", msgs[0]["content"])
	}
	if msgs[1]["content"] != "hello" {
		t.Errorf("user message should be kept, got %q", msgs[1]["content"])
	}
	if msgs[2]["content"] != "ok" {
		t.Errorf("assistant message should be kept, got %q", msgs[2]["content"])
	}
}

func TestBuildMessagesKeepsPoorSummariesWithoutFlag(t *testing.T) {
	ctx := NewNoAnchorContext()
	entries := []TapeEntry{
		NewCompactSummaryEntryWithSourceAndQuality("poor summary", CompactSummarySchemaVersion, "a1", "poor"),
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected poor summary + message when SkipPoorSummaries=false, got %d", len(msgs))
	}
	if msgs[0]["content"] != "poor summary" {
		t.Errorf("poor summary should be kept without flag, got %q", msgs[0]["content"])
	}
}

func TestBuildMessagesKeepsExplicitSystemEntry(t *testing.T) {
	ctx := NewNoAnchorContext()
	entries := []TapeEntry{
		NewSystemEntry("user system prompt"),
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
	}
	msgs := ctx.BuildMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "user system prompt" {
		t.Errorf("first message = %v, want system/user system prompt", msgs[0])
	}
}
