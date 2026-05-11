package core

import (
	"encoding/json"
	"testing"
)

func TestErrorPayloadError(t *testing.T) {
	ep := &ErrorPayload{Kind: ErrTool, Message: "bad args"}
	want := "tool: bad args"
	if ep.Error() != want {
		t.Errorf("got %q, want %q", ep.Error(), want)
	}
}

func TestErrorPayloadWithDetails(t *testing.T) {
	ep := &ErrorPayload{
		Kind:    ErrInvalidInput,
		Message: "validation failed",
		Details: map[string]any{"field": "name", "reason": "required"},
	}
	if ep.Details["field"] != "name" {
		t.Error("Details field mismatch")
	}
	if ep.Details["reason"] != "required" {
		t.Error("Details reason mismatch")
	}
}

func TestToolCallDataJSON(t *testing.T) {
	tc := ToolCallData{
		ID:       "call_1",
		Function: ToolCallFunction{Name: "echo", Arguments: `{"text":"hello"}`},
	}
	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatal(err)
	}
	var parsed ToolCallData
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ID != "call_1" || parsed.Function.Name != "echo" {
		t.Errorf("round-trip mismatch: %+v", parsed)
	}
}

func TestStreamEventKindValues(t *testing.T) {
	cases := map[StreamEventKind]string{
		StreamText:       "text",
		StreamToolCall:   "tool_call",
		StreamToolResult: "tool_result",
		StreamUsage:      "usage",
		StreamError:      "error",
		StreamFinal:      "final",
	}
	for kind, want := range cases {
		if string(kind) != want {
			t.Errorf("StreamEventKind %q != %q", kind, want)
		}
	}
}

func TestStreamEventDataAccess(t *testing.T) {
	event := StreamEvent{
		Kind: StreamText,
		Data: map[string]any{"delta": "hello"},
	}
	if event.Data["delta"] != "hello" {
		t.Error("Data access failed")
	}
	event.Data["extra"] = 42
	if event.Data["extra"] != 42 {
		t.Error("Data write failed")
	}
}

func TestToolAutoResultTextKind(t *testing.T) {
	r := ToolAutoResult{Kind: "text", Text: "hello world"}
	if r.Kind != "text" || r.Text != "hello world" {
		t.Error("text kind mismatch")
	}
	if r.ToolCalls != nil || r.Error != nil {
		t.Error("unexpected non-nil fields")
	}
}

func TestToolAutoResultToolsKind(t *testing.T) {
	r := ToolAutoResult{
		Kind: "tools",
		ToolCalls: []ToolCallData{
			{ID: "call_1", Function: ToolCallFunction{Name: "echo", Arguments: `{"text":"hi"}`}},
		},
		ToolResults: []any{"HI"},
	}
	if r.Kind != "tools" {
		t.Error("kind should be tools")
	}
	if len(r.ToolCalls) != 1 {
		t.Error("expected 1 tool call")
	}
	if len(r.ToolResults) != 1 {
		t.Error("expected 1 tool result")
	}
}

func TestToolAutoResultErrorKind(t *testing.T) {
	r := ToolAutoResult{
		Kind:  "error",
		Error: &ErrorPayload{Kind: ErrTool, Message: "failed"},
	}
	if r.Kind != "error" || r.Error == nil {
		t.Error("error kind mismatch")
	}
	if r.Error.Kind != ErrTool {
		t.Errorf("error payload kind = %q", r.Error.Kind)
	}
}

func TestToolExecutionNoError(t *testing.T) {
	ex := ToolExecution{
		ToolCalls: []ToolCallData{
			{ID: "call_1", Function: ToolCallFunction{Name: "echo", Arguments: `{"text":"hi"}`}},
		},
		ToolResults: []any{"HI"},
	}
	if ex.Error != nil {
		t.Error("unexpected error")
	}
	if len(ex.ToolCalls) != 1 {
		t.Error("expected 1 call")
	}
}

func TestToolExecutionWithError(t *testing.T) {
	ex := ToolExecution{
		ToolCalls:   []ToolCallData{{ID: "call_1"}},
		ToolResults: []any{map[string]any{"kind": "tool", "message": "failed"}},
		Error:       &ErrorPayload{Kind: ErrTool, Message: "handler error"},
	}
	if ex.Error == nil {
		t.Error("expected error")
	}
	if ex.Error.Kind != ErrTool {
		t.Errorf("error kind = %q", ex.Error.Kind)
	}
}
