package toolresult

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmpty(t *testing.T) {
	got := NormalizeEmpty("  \n", "shell")
	if !strings.Contains(got, "shell") {
		t.Fatalf("got %q", got)
	}
}

func TestGeneratePreview_NewlineBoundary(t *testing.T) {
	content := strings.Repeat("a", 80) + "\n" + strings.Repeat("b", 80)
	preview, more := GeneratePreview(content, 90)
	if !more {
		t.Fatal("expected hasMore")
	}
	if strings.Contains(preview, "bbbb") {
		t.Fatalf("preview should prefer newline cut, got %q", preview)
	}
}

func TestApplyTurnBudget_PersistsLargestUntilUnderBudget(t *testing.T) {
	ws := t.TempDir()
	m := NewManager(Policy{
		Workspace:        ws,
		DefaultMaxChars:  10_000,
		PerMessageBudget: 100_000,
	})
	body := strings.Repeat("q", 40_000)
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "a", "function": map[string]any{"name": "shell"}},
			map[string]any{"id": "b", "function": map[string]any{"name": "shell"}},
			map[string]any{"id": "c", "function": map[string]any{"name": "shell"}},
		}},
		{"role": "tool", "tool_call_id": "a", "content": body},
		{"role": "tool", "tool_call_id": "b", "content": body},
		{"role": "tool", "tool_call_id": "c", "content": body},
	}
	repl := m.ApplyTurnBudget("main", msgs)
	if len(repl) == 0 {
		t.Fatal("expected at least one budget replacement")
	}
	persisted := 0
	for i := 1; i < len(msgs); i++ {
		c, _ := msgs[i]["content"].(string)
		if strings.HasPrefix(c, PersistedOutputTag) {
			persisted++
		}
	}
	if persisted == 0 {
		t.Fatal("expected some persisted-output messages")
	}
}

func TestApplyTurnBudget_FrozenNotReplaced(t *testing.T) {
	ws := t.TempDir()
	m := NewManager(Policy{Workspace: ws, PerMessageBudget: 50_000})
	st := m.lockedState("t")
	st.Seen["frozen"] = struct{}{}
	body := strings.Repeat("x", 40_000)
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "frozen", "function": map[string]any{"name": "shell"}},
			map[string]any{"id": "fresh", "function": map[string]any{"name": "shell"}},
		}},
		{"role": "tool", "tool_call_id": "frozen", "content": body},
		{"role": "tool", "tool_call_id": "fresh", "content": body},
	}
	m.ApplyTurnBudget("t", msgs)
	frozen, _ := msgs[1]["content"].(string)
	if strings.HasPrefix(frozen, PersistedOutputTag) {
		t.Fatal("seen-but-not-replaced should stay frozen")
	}
	fresh, _ := msgs[2]["content"].(string)
	if !strings.HasPrefix(fresh, PersistedOutputTag) {
		t.Fatal("fresh row should be externalized under budget pressure")
	}
}

func TestMicrocompact_KeepRecent(t *testing.T) {
	m := NewManager(Policy{
		Microcompact: MicrocompactPolicy{
			Enabled:          true,
			KeepRecent:       2,
			CompactableTools: map[string]struct{}{"shell": {}},
		},
	})
	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "1", "function": map[string]any{"name": "shell"}},
			map[string]any{"id": "2", "function": map[string]any{"name": "shell"}},
			map[string]any{"id": "3", "function": map[string]any{"name": "shell"}},
		}},
		{"role": "tool", "tool_call_id": "1", "content": "one"},
		{"role": "tool", "tool_call_id": "2", "content": "two"},
		{"role": "tool", "tool_call_id": "3", "content": "three"},
	}
	m.PrepareWireMessages("main", msgs, time.Now())
	if msgs[1]["content"] != ToolResultClearedMessage {
		t.Fatalf("oldest should clear, got %v", msgs[1]["content"])
	}
	if msgs[3]["content"] != "three" {
		t.Fatalf("newest should remain, got %v", msgs[3]["content"])
	}
}

func TestMicrocompact_GapTriggered(t *testing.T) {
	past := time.Now().Add(-10 * time.Minute)
	m := NewManager(Policy{
		Microcompact: MicrocompactPolicy{
			Enabled:          true,
			KeepRecent:       1,
			CompactableTools: map[string]struct{}{"shell": {}},
			GapMinutes:       5,
		},
	})
	m.NoteAssistantTurn("main", past)

	msgs := []map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "1", "function": map[string]any{"name": "shell"}},
			map[string]any{"id": "2", "function": map[string]any{"name": "shell"}},
		}},
		{"role": "tool", "tool_call_id": "1", "content": "one"},
		{"role": "tool", "tool_call_id": "2", "content": "two"},
	}
	m.PrepareWireMessages("main", msgs, time.Now())
	cleared := 0
	for i := 1; i < len(msgs); i++ {
		if msgs[i]["content"] == ToolResultClearedMessage {
			cleared++
		}
	}
	if cleared != 1 {
		t.Fatalf("gap-triggered microcompact should clear all but keep_recent=1, cleared=%d", cleared)
	}
}

func TestCloneState(t *testing.T) {
	m := NewManager(Policy{})
	m.NoteAssistantTurn("t1", time.Now())
	m.MergeFlatMessages("t1", []map[string]any{
		{"role": "tool", "tool_call_id": "a", "content": PersistedOutputTag + " preview"},
	})

	c := m.CloneState()
	if c == m {
		t.Fatal("CloneState must return a new instance")
	}

	// Verify mcLastAssist copied
	c.mu.Lock()
	_, ok := c.mcLastAssist["t1"]
	c.mu.Unlock()
	if !ok {
		t.Fatal("cloned manager should copy mcLastAssist")
	}

	// Verify states copied
	c.mu.Lock()
	st, ok := c.states["t1"]
	c.mu.Unlock()
	if !ok {
		t.Fatal("cloned manager should copy states")
	}
	if _, seen := st.Seen["a"]; !seen {
		t.Fatal("cloned state should preserve Seen entries")
	}
	if _, repl := st.Replacements["a"]; !repl {
		t.Fatal("cloned state should preserve Replacements entries")
	}
}
