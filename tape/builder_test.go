package tape

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/config"
)

func TestContextBuilderReadMessagesLastAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	b := NewContextBuilder(store)

	msgs, err := b.ReadMessages("test_tape", NewLastAnchorContext())
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

func TestContextBuilderReadMessagesSoftBoundary(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	b := NewContextBuilder(store)

	ctx := NewSoftBoundaryContext(2)
	msgs, err := b.ReadMessages("test_tape", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0]["content"] != "task 2" {
		t.Errorf("first after-anchor content = %v, want task 2", msgs[0]["content"])
	}
	if msgs[1]["content"] != "task 1" {
		t.Errorf("second content = %v, want task 1", msgs[1]["content"])
	}
	if msgs[2]["content"] != "answer 1" {
		t.Errorf("third content = %v, want answer 1", msgs[2]["content"])
	}
}

func TestContextBuilderReadMessagesSoftBoundaryFallbackKeep(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	b := NewContextBuilder(store)

	// Expand the last anchor's raw-message window via fallback metadata.
	entries, _ := store.FetchAll("test_tape", nil)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "anchor" {
			state := map[string]any{}
			if s, ok := entries[i].Payload["state"].(map[string]any); ok {
				for k, v := range s {
					state[k] = v
				}
			}
			state["fallback_keep_before"] = 5
			entries[i].Payload["state"] = state
			break
		}
	}

	ctx := NewSoftBoundaryContext(1)
	msgs, err := b.ReadMessages("test_tape", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected fallback keep to include 4 messages, got %d", len(msgs))
	}
	if msgs[0]["content"] != "task 2" {
		t.Errorf("first after-anchor content = %v, want task 2", msgs[0]["content"])
	}
	if msgs[3]["content"] != "answer 1" {
		t.Errorf("last fallback content = %v, want answer 1", msgs[3]["content"])
	}
}

func TestContextBuilderReportsMissingAnchor(t *testing.T) {
	store := NewInMemoryTapeStore()
	seedEntries(store)
	b := NewContextBuilder(store)

	_, err := b.ReadMessages("test_tape", NewNamedAnchorContext("missing"))
	if err == nil {
		t.Fatal("expected error for missing anchor")
	}
}

func TestContextBuilderAppliesStrategy(t *testing.T) {
	store := NewInMemoryTapeStore()
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "assistant", "content": ""}))
	_ = store.Append("t", NewMessageEntry(map[string]any{"role": "assistant", "content": "world"}))

	b := NewContextBuilder(store)
	ctx := NewNoAnchorContext()
	ctx.Strategy = config.CompactStrategySnip

	msgs, err := b.ReadMessages("t", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected snip to drop empty assistant message, got %d messages", len(msgs))
	}
	if msgs[0]["content"] != "hello" || msgs[1]["content"] != "world" {
		t.Errorf("unexpected messages: %v", msgs)
	}
}

func TestGraduatedTrimOldToolResults(t *testing.T) {
	long := strings.Repeat("x", 1000)
	msgs := []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c1"}}},
		{"role": "tool", "tool_call_id": "c1", "content": long},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c2"}}},
		{"role": "tool", "tool_call_id": "c2", "content": long},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c3"}}},
		{"role": "tool", "tool_call_id": "c3", "content": long},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c4"}}},
		{"role": "tool", "tool_call_id": "c4", "content": long},
	}
	ctx := &TapeContext{
		KeepSummary:        true,
		GraduatedTrim:      true,
		RecentToolResults:  2,
		OldToolResultRatio: 0.25,
		ToolResultMaxChars: 400,
	}
	out := applyProgressiveTrim(msgs, ctx)

	// First two tool messages (oldest) should be trimmed; last two kept intact.
	trimmed1, _ := out[1]["content"].(string)
	trimmed2, _ := out[3]["content"].(string)
	kept3, _ := out[5]["content"].(string)
	kept4, _ := out[7]["content"].(string)

	if !strings.Contains(trimmed1, "[... truncated") {
		t.Errorf("oldest tool result should be trimmed, got %q", trimmed1[:min(50, len(trimmed1))])
	}
	if !strings.Contains(trimmed2, "[... truncated") {
		t.Errorf("second tool result should be trimmed, got %q", trimmed2[:min(50, len(trimmed2))])
	}
	if kept3 != long {
		t.Errorf("recent tool result should be kept intact")
	}
	if kept4 != long {
		t.Errorf("most recent tool result should be kept intact")
	}
	// Trimmed budget ~ 400*0.25 = 100 runes + marker; well below original 1000.
	if len([]rune(trimmed1)) >= 1000 {
		t.Errorf("trimmed content should be shorter than original")
	}
}

func TestMicrocompactOldToolResults(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c1"}}},
		{"role": "tool", "tool_call_id": "c1", "content": "old big result"},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c2"}}},
		{"role": "tool", "tool_call_id": "c2", "content": "fresh big result"},
	}
	ctx := &TapeContext{KeepSummary: true, ContextMicrocompact: true}
	out := applyProgressiveTrim(msgs, ctx)

	if out[1]["content"] != "[Old tool result content cleared]" {
		t.Errorf("old tool result should be cleared, got %v", out[1]["content"])
	}
	if out[3]["content"] != "fresh big result" {
		t.Errorf("recent tool result should be kept, got %v", out[3]["content"])
	}
	if out[1]["role"] != "tool" || out[1]["tool_call_id"] != "c1" {
		t.Errorf("cleared tool message should retain structure: %v", out[1])
	}
}

func TestGraduatedTrimNoBudgetNoop(t *testing.T) {
	msgs := []map[string]any{
		{"role": "tool", "tool_call_id": "c1", "content": "data"},
	}
	// ToolResultMaxChars == 0 → no trimming.
	ctx := &TapeContext{GraduatedTrim: true, RecentToolResults: 3, OldToolResultRatio: 0.25}
	out := applyProgressiveTrim(msgs, ctx)
	if out[0]["content"] != "data" {
		t.Errorf("expected no trim without budget, got %v", out[0]["content"])
	}
}

func TestSnipFrontUnits_DropsSafeUnits(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "sysprompt"},
		{"role": "user", "content": "old user 1"},
		{"role": "assistant", "content": "old reply 1"},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c1"}}},
		{"role": "tool", "tool_call_id": "c1", "content": "old tool result"},
		{"role": "user", "content": "keep me recent"},
		{"role": "assistant", "content": "recent reply"},
	}
	// Drop 3 units; last 2 turns protected.
	out := snipFrontUnits(msgs, 3, 2)

	// System must survive.
	if out[0]["role"] != "system" {
		t.Errorf("system message must not be snipped: %v", out[0])
	}
	// The protected recent turns must survive.
	var contents []string
	for _, m := range out {
		if c, ok := m["content"].(string); ok && c != "" {
			contents = append(contents, c)
		}
	}
	if !containsStr(contents, "keep me recent") || !containsStr(contents, "recent reply") {
		t.Errorf("recent turns should be protected, got: %v", contents)
	}
	// Old content should be gone.
	if containsStr(contents, "old user 1") || containsStr(contents, "old tool result") {
		t.Errorf("old messages should be snipped, got: %v", contents)
	}
	// No dangling tool message: every tool message must have a preceding
	// assistant with tool_calls.
	for i, m := range out {
		if m["role"] == "tool" {
			if i == 0 || out[i-1]["role"] != "assistant" {
				if _, ok := out[i-1]["tool_calls"]; !ok && out[i-1]["role"] == "assistant" {
					t.Errorf("dangling tool message at %d", i)
				}
			}
		}
	}
}

func TestSnipFrontUnits_NoBreakToolPairing(t *testing.T) {
	msgs := []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c1"}}},
		{"role": "tool", "tool_call_id": "c1", "content": "result"},
		{"role": "assistant", "content": "final"},
	}
	// Dropping 1 unit must drop the assistant+tool pair together, leaving "final".
	out := snipFrontUnits(msgs, 1, 1)
	if len(out) != 1 {
		t.Fatalf("expected 1 message after snipping the tool-call pair, got %d: %v", len(out), out)
	}
	if out[0]["content"] != "final" {
		t.Errorf("expected final assistant message to survive, got %v", out[0])
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestBuildMessages_KeepsCompletedToolsOnInterrupt(t *testing.T) {
	entries := []TapeEntry{
		NewExecStartEntry("exec-A", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "old prompt"}),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "shell"}}}}),
		NewMessageEntry(map[string]any{"role": "tool", "tool_call_id": "c1", "content": "old result"}),
		NewEventEntry(EventRunInterrupted, map[string]any{"exec_id": "exec-A", "reason": "cancelled"}),
		NewExecStartEntry("exec-B", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "new prompt"}),
	}

	msgs := buildMessages(entries, nil)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0]["content"] != "old prompt" {
		t.Errorf("first message = %#v, want old prompt", msgs[0])
	}
	if msgs[3]["content"] != InterruptedSystemNotice {
		t.Errorf("notice = %#v", msgs[3])
	}
	if msgs[4]["content"] != "new prompt" {
		t.Errorf("last message = %#v, want new prompt", msgs[4])
	}
}

func TestBuildMessages_PreemptSuppressesNotice(t *testing.T) {
	entries := []TapeEntry{
		NewExecStartEntry("exec-A", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "old prompt"}),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "shell"}}}}),
		NewMessageEntry(map[string]any{"role": "tool", "tool_call_id": "c1", "content": "old result"}),
		NewEventEntry(EventRunInterrupted, map[string]any{"exec_id": "exec-A", "reason": "preempted", "suppress_notice": true}),
		NewExecStartEntry("exec-B", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "nginx-1.29.1"}),
	}

	msgs := buildMessages(entries, nil)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %#v", len(msgs), msgs)
	}
	for _, m := range msgs {
		if m["content"] == InterruptedSystemNotice {
			t.Fatalf("preempt should suppress notice, got %#v", msgs)
		}
	}
	if msgs[3]["content"] != "nginx-1.29.1" {
		t.Errorf("last message = %#v", msgs[3])
	}
}

func TestBuildMessages_RepairsOrphanToolCalls(t *testing.T) {
	entries := []TapeEntry{
		NewExecStartEntry("exec-A", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "run tools"}),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "shell"}},
			map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "shell"}},
		}}),
		NewMessageEntry(map[string]any{"role": "tool", "tool_call_id": "c1", "content": "done"}),
		NewEventEntry(EventRunInterrupted, map[string]any{"exec_id": "exec-A", "reason": "cancelled"}),
	}

	msgs := buildMessages(entries, nil)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %#v", len(msgs), msgs)
	}
	if msgs[3]["tool_call_id"] != "c2" {
		t.Errorf("expected synthetic for c2, got %#v", msgs[3])
	}
	if msgs[3]["content"] != syntheticToolInterruptResult {
		t.Errorf("synthetic content = %v", msgs[3]["content"])
	}
}

func TestBuildMessages_NoInterruptKeepsMessages(t *testing.T) {
	entries := []TapeEntry{
		NewExecStartEntry("exec-A", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "hello"}),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "hi there"}),
	}

	msgs := buildMessages(entries, nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0]["content"] != "hello" || msgs[1]["content"] != "hi there" {
		t.Errorf("unexpected messages: %#v", msgs)
	}
}

func TestBuildMessages_MultipleInterrupts(t *testing.T) {
	entries := []TapeEntry{
		NewExecStartEntry("exec-A", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "first"}),
		NewEventEntry(EventRunInterrupted, map[string]any{"exec_id": "exec-A", "reason": "preempted", "suppress_notice": true}),
		NewExecStartEntry("exec-B", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "second"}),
		NewEventEntry(EventRunInterrupted, map[string]any{"exec_id": "exec-B", "reason": "preempted", "suppress_notice": true}),
		NewExecStartEntry("exec-C", "agent", nil),
		NewMessageEntry(map[string]any{"role": "user", "content": "third"}),
	}

	msgs := buildMessages(entries, nil)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0]["content"] != "first" || msgs[1]["content"] != "second" || msgs[2]["content"] != "third" {
		t.Errorf("unexpected messages: %#v", msgs)
	}
}
