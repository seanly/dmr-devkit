package handoff

import (
	"context"
	"errors"
	"testing"

	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

type mockAgent struct {
	focus   string
	summary string
	err     error
}

func (m *mockAgent) CompactTapeWithFocus(_ context.Context, _, focus string) (string, error) {
	m.focus = focus
	if m.err != nil {
		return "", m.err
	}
	return m.summary, nil
}

func TestNewToolSpec(t *testing.T) {
	a := &mockAgent{summary: "summary"}
	tt := NewTool(a)
	if tt.Spec.Name != "handoff" {
		t.Errorf("name = %q, want handoff", tt.Spec.Name)
	}
	if tt.Spec.Group != tool.ToolGroupCore {
		t.Errorf("group = %q, want core", tt.Spec.Group)
	}
	if !tt.Spec.AlwaysLoad {
		t.Error("expected AlwaysLoad = true")
	}
}

func TestHandleHandoff_WithFocus(t *testing.T) {
	a := &mockAgent{summary: "focused summary"}
	tt := NewTool(a)
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	ctx := tool.NewToolContext(context.Background(), "cli:main", "")
	ctx.State[tool.StateKeyTapeManager] = tm

	out, err := tt.Handler(ctx, map[string]any{"focus": "auth module"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.focus != "auth module" {
		t.Errorf("focus = %q, want auth module", a.focus)
	}
	if out == nil {
		t.Fatal("expected output")
	}
	s, ok := out.(string)
	if !ok || !containsSubstring(s, "focused summary") {
		t.Fatalf("unexpected output: %v", out)
	}
}

func TestHandleHandoff_NoFocus(t *testing.T) {
	a := &mockAgent{summary: "general summary"}
	tt := NewTool(a)
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	ctx := tool.NewToolContext(context.Background(), "cli:main", "")
	ctx.State[tool.StateKeyTapeManager] = tm

	out, err := tt.Handler(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.focus != "" {
		t.Errorf("focus = %q, want empty", a.focus)
	}
	s, ok := out.(string)
	if !ok || !containsSubstring(s, "general summary") {
		t.Fatalf("unexpected output: %v", out)
	}
}

func TestHandleHandoff_MissingTape(t *testing.T) {
	a := &mockAgent{summary: "summary"}
	tt := NewTool(a)
	ctx := tool.NewToolContext(context.Background(), "", "")
	ctx.State[tool.StateKeyTapeManager] = tape.NewTapeManager(tape.NewInMemoryTapeStore())

	_, err := tt.Handler(ctx, nil)
	if err == nil {
		t.Fatal("expected error for missing tape")
	}
}

func TestHandleHandoff_MissingTapeManager(t *testing.T) {
	a := &mockAgent{summary: "summary"}
	tt := NewTool(a)
	ctx := tool.NewToolContext(context.Background(), "cli:main", "")

	_, err := tt.Handler(ctx, nil)
	if err == nil {
		t.Fatal("expected error for missing tape manager")
	}
}

func TestHandleHandoff_AgentError(t *testing.T) {
	a := &mockAgent{err: errors.New("boom")}
	tt := NewTool(a)
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	ctx := tool.NewToolContext(context.Background(), "cli:main", "")
	ctx.State[tool.StateKeyTapeManager] = tm

	_, err := tt.Handler(ctx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
