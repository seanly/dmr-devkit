package tool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/core"
)

func echoHandler(_ *ToolContext, args map[string]any) (any, error) {
	text, _ := args["text"].(string)
	return text, nil
}

func echoTool() *Tool {
	return &Tool{
		Spec: ToolSpec{
			Name: "echo",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
		Handler: echoHandler,
	}
}

func TestExecuteSchemaRequiredMissing(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{}`}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error for missing required text")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q, want %q", result.Error.Kind, core.ErrInvalidInput)
	}
}

func TestExecuteSchemaRequiredStringEmpty(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"text":""}`}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error for empty required text")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q, want %q", result.Error.Kind, core.ErrInvalidInput)
	}
}

func TestExecuteSchemaEnumInvalid(t *testing.T) {
	modeTool := &Tool{
		Spec: ToolSpec{
			Name: "mode",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"level": map[string]any{
						"type": "string",
						"enum": []string{"low", "high"},
					},
				},
				"required": []string{"level"},
			},
		},
		Handler: func(_ *ToolContext, args map[string]any) (any, error) {
			return args["level"], nil
		},
	}
	ts, _ := NormalizeTools([]*Tool{modeTool})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "mode", Arguments: `{"level":"medium"}`}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error for invalid enum value")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q, want %q", result.Error.Kind, core.ErrInvalidInput)
	}
}

func TestExecuteSchemaTypeMismatch(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"text":123}`}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error for wrong argument type")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q, want %q", result.Error.Kind, core.ErrInvalidInput)
	}
}

func TestExecuteArgsJSONNullBecomesEmptyObject(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `null`}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error: null args should not satisfy required fields")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q, want %q", result.Error.Kind, core.ErrInvalidInput)
	}
}

func TestExecuteSimpleTool(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"text":"hello"}`}},
		},
		ts, nil,
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	want := "[LIVE DATA from echo]\nhello"
	if len(result.ToolResults) != 1 || result.ToolResults[0] != want {
		t.Errorf("result = %v, want %q", result.ToolResults, want)
	}
}

func TestExecuteToolWithJSONStringArgs(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"text":"world"}`}},
		},
		ts, nil,
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	want := "[LIVE DATA from echo]\nworld"
	if result.ToolResults[0] != want {
		t.Errorf("result = %q, want %q", result.ToolResults[0], want)
	}
}

func TestExecuteToolNotFound(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "missing", Arguments: "{}"}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error for missing tool")
	}
	if result.Error.Kind != core.ErrNotFound {
		t.Errorf("error kind = %q", result.Error.Kind)
	}
}

func TestExecuteToolHandlerError(t *testing.T) {
	failTool := &Tool{
		Spec: ToolSpec{Name: "fail"},
		Handler: func(_ *ToolContext, args map[string]any) (any, error) {
			return nil, fmt.Errorf("something broke")
		},
	}
	ts, _ := NormalizeTools([]*Tool{failTool})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "fail", Arguments: "{}"}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.Kind != core.ErrTool {
		t.Errorf("error kind = %q", result.Error.Kind)
	}
}

func TestExecuteToolWithContext(t *testing.T) {
	contextTool := &Tool{
		Spec:        ToolSpec{Name: "write_note"},
		NeedContext: true,
		Handler: func(ctx *ToolContext, args map[string]any) (any, error) {
			title, _ := args["title"].(string)
			ctx.State["title"] = title
			return title, nil
		},
	}
	ts, _ := NormalizeTools([]*Tool{contextTool})
	executor := NewToolExecutor()

	ctx := NewToolContext(context.Background(), "ops", "run-1")
	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "write_note", Arguments: `{"title":"hello"}`}},
		},
		ts, ctx,
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	want := "[LIVE DATA from write_note]\nhello"
	if result.ToolResults[0] != want {
		t.Errorf("result = %q, want %q", result.ToolResults[0], want)
	}
	if ctx.State["title"] != "hello" {
		t.Error("context state not modified")
	}
}

func TestExecuteToolContextMissing(t *testing.T) {
	contextTool := &Tool{
		Spec:        ToolSpec{Name: "write_note"},
		NeedContext: true,
		Handler: func(ctx *ToolContext, args map[string]any) (any, error) {
			return "ok", nil
		},
	}
	ts, _ := NormalizeTools([]*Tool{contextTool})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "write_note", Arguments: `{"title":"hello"}`}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error when context missing")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q", result.Error.Kind)
	}
}

func TestExecuteMultipleTools(t *testing.T) {
	addTool := &Tool{
		Spec: ToolSpec{Name: "add"},
		Handler: func(_ *ToolContext, args map[string]any) (any, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return a + b, nil
		},
	}
	ts, _ := NormalizeTools([]*Tool{echoTool(), addTool})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: `{"text":"hi"}`}},
			{ID: "call_2", Function: core.ToolCallFunction{Name: "add", Arguments: `{"a":1,"b":2}`}},
		},
		ts, nil,
	)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.ToolResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.ToolResults))
	}
	wantEcho := "[LIVE DATA from echo]\nhi"
	if result.ToolResults[0] != wantEcho {
		t.Errorf("echo result = %q, want %q", result.ToolResults[0], wantEcho)
	}
	if result.ToolResults[1] != float64(3) {
		t.Errorf("add result = %v", result.ToolResults[1])
	}
}

func TestExecuteToolContextStateModification(t *testing.T) {
	counterTool := &Tool{
		Spec:        ToolSpec{Name: "increment"},
		NeedContext: true,
		Handler: func(ctx *ToolContext, args map[string]any) (any, error) {
			count, _ := ctx.State["count"].(int)
			count++
			ctx.State["count"] = count
			return count, nil
		},
	}
	ts, _ := NormalizeTools([]*Tool{counterTool})
	executor := NewToolExecutor()

	ctx := NewToolContext(context.Background(), "t", "r")
	ctx.State["count"] = 0

	executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "increment", Arguments: "{}"}},
		},
		ts, ctx,
	)
	executor.Execute(
		[]core.ToolCallData{
			{ID: "call_2", Function: core.ToolCallFunction{Name: "increment", Arguments: "{}"}},
		},
		ts, ctx,
	)

	if ctx.State["count"] != 2 {
		t.Errorf("count = %v, want 2", ctx.State["count"])
	}
}

func TestExecuteSchemaOnlyTool(t *testing.T) {
	schemaOnly := &Tool{
		Spec:    ToolSpec{Name: "schema_only"},
		Handler: nil,
	}
	ts, _ := NormalizeTools([]*Tool{schemaOnly})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "schema_only", Arguments: "{}"}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error for schema-only tool")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q", result.Error.Kind)
	}
}

func TestExecuteInvalidJSON(t *testing.T) {
	ts, _ := NormalizeTools([]*Tool{echoTool()})
	executor := NewToolExecutor()

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "echo", Arguments: "not json"}},
		},
		ts, nil,
	)

	if result.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if result.Error.Kind != core.ErrInvalidInput {
		t.Errorf("error kind = %q", result.Error.Kind)
	}
}

func TestExecuteParallelSubagentsRespectsContextCancel(t *testing.T) {
	slowSubagent := &Tool{
		Spec: ToolSpec{Name: "subagent"},
		Handler: func(_ *ToolContext, args map[string]any) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return "done", nil
		},
	}
	ts, _ := NormalizeTools([]*Tool{slowSubagent})
	executor := NewToolExecutor()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	toolCtx := NewToolContext(ctx, "test", "run-1")

	result := executor.Execute(
		[]core.ToolCallData{
			{ID: "call_1", Function: core.ToolCallFunction{Name: "subagent", Arguments: "{}"}},
			{ID: "call_2", Function: core.ToolCallFunction{Name: "subagent", Arguments: "{}"}},
		},
		ts, toolCtx,
	)

	if result.Error == nil {
		t.Fatal("expected cancellation error")
	}
	if result.Error.Kind != core.ErrTool {
		t.Errorf("error kind = %q, want %q", result.Error.Kind, core.ErrTool)
	}
	if len(result.ToolResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.ToolResults))
	}
	// Give background goroutines a moment to finish so the race detector is happy.
	time.Sleep(250 * time.Millisecond)
}

func TestTruncateToolValueForLog_RedactsCredentialBindings(t *testing.T) {
	input := map[string]any{
		"path":                "/tmp",
		"credential_bindings": []any{map[string]any{"credential_id": "prod-db", "env": "PGPASSWORD"}},
		"_runtime_env":        map[string]string{"PGPASSWORD": "secret123"},
	}
	out := truncateToolValueForLog(input, 1000)
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if outMap["credential_bindings"] != "(redacted)" {
		t.Errorf("credential_bindings should be redacted, got %v", outMap["credential_bindings"])
	}
	if outMap["_runtime_env"] != "(redacted)" {
		t.Errorf("_runtime_env should be redacted, got %v", outMap["_runtime_env"])
	}
	if outMap["path"] != "/tmp" {
		t.Errorf("path should be preserved, got %v", outMap["path"])
	}
}
