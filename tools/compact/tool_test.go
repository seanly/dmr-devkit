package compact

import (
	"context"
	"errors"
	"testing"

	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

type mockAgent struct {
	summary      string
	err          error
	canCompact   bool
}

func (m *mockAgent) CanCompactTool(_ string) bool {
	return m.canCompact
}

func (m *mockAgent) CompactTape(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.summary, nil
}

func TestNewToolSpec(t *testing.T) {
	a := &mockAgent{summary: "summary", canCompact: true}
	tt := NewTool(a)
	if tt.Spec.Name != "compact" {
		t.Errorf("name = %q, want compact", tt.Spec.Name)
	}
	if tt.Spec.Group != tool.ToolGroupCore {
		t.Errorf("group = %q, want core", tt.Spec.Group)
	}
	if !tt.Spec.AlwaysLoad {
		t.Error("expected AlwaysLoad = true")
	}
}

func TestHandleCompact(t *testing.T) {
	a := &mockAgent{summary: "general summary", canCompact: true}
	tt := NewTool(a)
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	ctx := tool.NewToolContext(context.Background(), "cli:main", "")
	ctx.State[tool.StateKeyTapeManager] = tm

	out, err := tt.Handler(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := out.(string)
	if !ok || !containsSubstring(s, "general summary") {
		t.Fatalf("unexpected output: %v", out)
	}
}

func TestHandleCompact_MissingTape(t *testing.T) {
	a := &mockAgent{summary: "summary", canCompact: true}
	tt := NewTool(a)
	ctx := tool.NewToolContext(context.Background(), "", "")
	ctx.State[tool.StateKeyTapeManager] = tape.NewTapeManager(tape.NewInMemoryTapeStore())

	_, err := tt.Handler(ctx, nil)
	if err == nil {
		t.Fatal("expected error for missing tape")
	}
}

func TestHandleCompact_MissingTapeManager(t *testing.T) {
	a := &mockAgent{summary: "summary", canCompact: true}
	tt := NewTool(a)
	ctx := tool.NewToolContext(context.Background(), "cli:main", "")

	_, err := tt.Handler(ctx, nil)
	if err == nil {
		t.Fatal("expected error for missing tape manager")
	}
}

func TestHandleCompact_AgentError(t *testing.T) {
	a := &mockAgent{err: errors.New("boom"), canCompact: true}
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

func TestHandleCompact_CooldownBlocks(t *testing.T) {
	a := &mockAgent{summary: "summary", canCompact: false}
	tt := NewTool(a)
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	ctx := tool.NewToolContext(context.Background(), "cli:main", "")
	ctx.State[tool.StateKeyTapeManager] = tm

	out, err := tt.Handler(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := out.(string)
	if !ok || !containsSubstring(s, "already performed very recently") {
		t.Fatalf("expected cooldown message, got: %v", out)
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
