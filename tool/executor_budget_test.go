package tool

import (
	"testing"

	"github.com/seanly/dmr-devkit/core"
)

func TestToolExecutor_DuplicateBudget(t *testing.T) {
	ex := NewToolExecutor()
	ex.MaxDuplicateToolCalls = 2
	ex.MaxTotalToolCalls = 10

	toolSet := &ToolSet{
		Runnable: map[string]*Tool{
			"echo": {
				Spec: ToolSpec{Name: "echo"},
				Handler: func(ctx *ToolContext, args map[string]any) (any, error) {
					return args["msg"], nil
				},
			},
		},
	}
	ctx := NewToolContext(nil, "tape1", "")

	calls := []core.ToolCallData{
		{ID: "1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"msg":"hi"}`}},
	}

	for i := 0; i < 2; i++ {
		res := ex.Execute(calls, toolSet, ctx)
		if len(res.ToolResults) != 1 {
			t.Fatalf("iteration %d: expected 1 result, got %d", i, len(res.ToolResults))
		}
		if got, ok := res.ToolResults[0].(string); !ok || got != "[LIVE DATA from echo]\nhi" {
			t.Fatalf("iteration %d: expected tagged hi, got %v", i, res.ToolResults[0])
		}
	}

	// Third identical call should be denied.
	res := ex.Execute(calls, toolSet, ctx)
	if len(res.ToolResults) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.ToolResults))
	}
	m, ok := res.ToolResults[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res.ToolResults[0])
	}
	if m["kind"] != "denied" {
		t.Fatalf("expected denied, got %v", m)
	}
}

func TestToolExecutor_TotalBudget(t *testing.T) {
	ex := NewToolExecutor()
	ex.MaxDuplicateToolCalls = 0
	ex.MaxTotalToolCalls = 2

	toolSet := &ToolSet{
		Runnable: map[string]*Tool{
			"echo": {
				Spec: ToolSpec{Name: "echo"},
				Handler: func(ctx *ToolContext, args map[string]any) (any, error) {
					return args["msg"], nil
				},
			},
		},
	}
	ctx := NewToolContext(nil, "tape2", "")

	calls := []core.ToolCallData{
		{ID: "1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"msg":"a"}`}},
		{ID: "2", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"msg":"b"}`}},
	}

	res := ex.Execute(calls, toolSet, ctx)
	if len(res.ToolResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.ToolResults))
	}

	// One more call should exceed total budget.
	res = ex.Execute([]core.ToolCallData{
		{ID: "3", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"msg":"c"}`}},
	}, toolSet, ctx)
	if len(res.ToolResults) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.ToolResults))
	}
	m, ok := res.ToolResults[0].(map[string]any)
	if !ok || m["kind"] != "denied" {
		t.Fatalf("expected denied, got %v", res.ToolResults[0])
	}
}

func TestToolExecutor_ResetBudget(t *testing.T) {
	ex := NewToolExecutor()
	ex.MaxDuplicateToolCalls = 1
	ex.MaxTotalToolCalls = 0

	toolSet := &ToolSet{
		Runnable: map[string]*Tool{
			"echo": {
				Spec: ToolSpec{Name: "echo"},
				Handler: func(ctx *ToolContext, args map[string]any) (any, error) {
					return args["msg"], nil
				},
			},
		},
	}
	ctx := NewToolContext(nil, "tape3", "")

	calls := []core.ToolCallData{
		{ID: "1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"msg":"x"}`}},
	}
	ex.Execute(calls, toolSet, ctx)
	ex.Execute(calls, toolSet, ctx)

	res := ex.Execute(calls, toolSet, ctx)
	m := res.ToolResults[0].(map[string]any)
	if m["kind"] != "denied" {
		t.Fatalf("expected denied before reset, got %v", m)
	}

	ex.ResetBudget("tape3")
	res = ex.Execute(calls, toolSet, ctx)
	if got := res.ToolResults[0].(string); got != "[LIVE DATA from echo]\nx" {
		t.Fatalf("expected tagged execution after reset, got %v", res.ToolResults[0])
	}
}
