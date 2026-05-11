package tool

import (
	"context"
	"testing"
)

func TestToolContextDefaults(t *testing.T) {
	ctx := NewToolContext(context.Background(), "tape1", "run1")
	if ctx.Tape != "tape1" {
		t.Errorf("Tape = %q", ctx.Tape)
	}
	if ctx.RunID != "run1" {
		t.Errorf("RunID = %q", ctx.RunID)
	}
	if ctx.Meta == nil {
		t.Error("Meta should not be nil")
	}
	if ctx.State == nil {
		t.Error("State should not be nil")
	}
}

func TestToolContextWorkspace(t *testing.T) {
	ctx := NewToolContext(context.Background(), "t", "r")

	// Default empty
	if ctx.Workspace != "" {
		t.Errorf("default Workspace = %q, want empty", ctx.Workspace)
	}
	if ctx.GetCwd() != "" {
		t.Errorf("GetCwd() = %q, want empty", ctx.GetCwd())
	}

	// Set typed field
	ctx.Workspace = "/workspace"
	if ctx.GetCwd() != "/workspace" {
		t.Errorf("GetCwd() = %q, want /workspace", ctx.GetCwd())
	}
}

func TestToolContextWorkspace_BackwardCompat(t *testing.T) {
	ctx := NewToolContext(context.Background(), "t", "r")

	// Legacy State injection still works
	ctx.State[StateKeyRuntimeWorkspace] = "/legacy"
	if ctx.GetCwd() != "/legacy" {
		t.Errorf("GetCwd() = %q, want /legacy", ctx.GetCwd())
	}

	// Typed field takes priority over legacy State
	ctx.Workspace = "/typed"
	if ctx.GetCwd() != "/typed" {
		t.Errorf("GetCwd() = %q, want /typed", ctx.GetCwd())
	}
}

func TestToolContextStateSharedAcrossCalls(t *testing.T) {
	ctx := NewToolContext(context.Background(), "t", "r")
	ctx.State["key"] = "value1"

	// Simulating a second handler access
	if ctx.State["key"] != "value1" {
		t.Error("state not shared")
	}
	ctx.State["key"] = "value2"
	if ctx.State["key"] != "value2" {
		t.Error("state not updated")
	}
}
