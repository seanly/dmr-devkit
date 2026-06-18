package a2ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

func TestFixJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, got string)
	}{
		{
			name:  "valid json",
			input: `{"key": "value"}`,
			check: func(t *testing.T, got string) {
				if got != `{"key": "value"}` {
					t.Errorf("got %q", got)
				}
			},
		},
		{
			name:  "markdown fences",
			input: "```json\n{\"key\": \"value\"}\n```",
			check: func(t *testing.T, got string) {
				if got != `{"key": "value"}` {
					t.Errorf("got %q", got)
				}
			},
		},
		{
			name:  "single quotes",
			input: `{'key': 'value'}`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
				if m["key"] != "value" {
					t.Errorf("got %v", m)
				}
			},
		},
		{
			name:  "unquoted keys",
			input: `{key: "value", num: 1}`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
				if m["key"] != "value" || fmt.Sprint(m["num"]) != "1" {
					t.Errorf("got %v", m)
				}
			},
		},
		{
			name:  "trailing comma",
			input: `{"key": "value",}`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
		{
			name:  "truncated braces",
			input: `{"key": "value"`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
		{
			name:  "combined issues",
			input: "```json\n{key: 'value', items: [1, 2,],}\n```",
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
				if m["key"] != "value" {
					t.Errorf("got %v", m)
				}
			},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:  "single quotes inside double-quoted string",
			input: `{"key": "don't change"}`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
				if m["key"] != "don't change" {
					t.Errorf("got %v", m)
				}
			},
		},
		{
			name:  "trailing comma in array",
			input: `[1, 2, 3,]`,
			check: func(t *testing.T, got string) {
				var a []int
				if err := json.Unmarshal([]byte(got), &a); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
		{
			name:  "truncated brackets",
			input: `[1, 2, 3`,
			check: func(t *testing.T, got string) {
				var a []int
				if err := json.Unmarshal([]byte(got), &a); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
		{
			name:  "nested braces truncated",
			input: `{"a": {"b": 1`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FixJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FixJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestMustCompactJSON(t *testing.T) {
	if got := mustCompactJSON(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Errorf("got %q", got)
	}
	if got := mustCompactJSON(nil); got != "null" {
		t.Errorf("got %q", got)
	}
}

func TestJoinLines(t *testing.T) {
	got := joinLines([]string{"a", "b", "c"})
	if got != "a\n\nb\n\nc" {
		t.Errorf("got %q", got)
	}
	if joinLines(nil) != "" {
		t.Errorf("expected empty string")
	}
}

func TestHasSuffix(t *testing.T) {
	if !hasSuffix("hello.json", ".json") {
		t.Errorf("expected true")
	}
	if hasSuffix("hello", ".json") {
		t.Errorf("expected false")
	}
	if !hasSuffix(".json", ".json") {
		t.Errorf("expected true")
	}
	if !hasSuffix("", "") {
		t.Errorf("empty suffix should match empty string")
	}
}

func TestIsInsideString(t *testing.T) {
	if isInsideString(`{"key": "value"}`, 6) {
		t.Errorf("position 6 (space after colon) should be outside string")
	}
	if !isInsideString(`{"key": "value"}`, 10) {
		t.Errorf("position 10 (inside value string) should be inside string")
	}
	if !isInsideString(`{"x": "1"}`, 2) {
		t.Errorf("position 2 (inside key) should be inside string")
	}
	if !isInsideString(`{"x": "1"}`, 8) {
		t.Errorf("position 8 (inside value) should be inside string")
	}
}

func TestExampleSystemPrompt(t *testing.T) {
	if ExampleSystemPrompt() == "" {
		t.Errorf("expected non-empty prompt")
	}
}

func TestCatalogRenderAsLLMInstructions(t *testing.T) {
	c := &Catalog{
		CatalogID:     "test",
		S2CSchema:     map[string]any{"a": 1},
		CommonTypes:   map[string]any{"b": 2},
		CatalogSchema: map[string]any{"c": 3},
	}
	out := c.RenderAsLLMInstructions()
	if out == "" {
		t.Errorf("expected non-empty instructions")
	}
	if !strings.Contains(out, SchemaBlockStart) {
		t.Errorf("expected schema block start")
	}
	if !strings.Contains(out, SchemaBlockEnd) {
		t.Errorf("expected schema block end")
	}
	if !strings.Contains(out, `"a":1`) {
		t.Errorf("expected s2c schema content")
	}
	if !strings.Contains(out, `"b":2`) {
		t.Errorf("expected common types content")
	}
}

func TestCatalogGenerateSystemPrompt(t *testing.T) {
	c := &Catalog{
		CatalogID:     "test",
		S2CSchema:     map[string]any{"a": 1},
		CatalogSchema: map[string]any{"c": 3},
	}
	out := c.GenerateSystemPrompt("role", "workflow", "ui", true, false)
	if !strings.Contains(out, "role") {
		t.Errorf("expected role")
	}
	if !strings.Contains(out, "workflow") {
		t.Errorf("expected workflow")
	}
	if !strings.Contains(out, "ui") {
		t.Errorf("expected ui")
	}
	if !strings.Contains(out, SchemaBlockStart) {
		t.Errorf("expected schema")
	}
	if strings.Contains(out, "Examples") {
		t.Errorf("unexpected examples")
	}

	c.Examples = "ex"
	out2 := c.GenerateSystemPrompt("role", "workflow", "ui", true, true)
	if !strings.Contains(out2, "Examples") {
		t.Errorf("expected examples")
	}
}

func TestValidateMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantErr string
	}{
		{
			name: "createSurface missing surfaceId",
			msg: &Message{
				CreateSurface: &CreateSurface{CatalogID: "c"},
			},
			wantErr: "createSurface.surfaceId is required",
		},
		{
			name: "createSurface missing catalogId",
			msg: &Message{
				CreateSurface: &CreateSurface{SurfaceID: "s"},
			},
			wantErr: "createSurface.catalogId is required",
		},
		{
			name: "updateComponents missing surfaceId",
			msg: &Message{
				UpdateComponents: &UpdateComponents{
					Components: []Component{{ID: "root"}},
				},
			},
			wantErr: "updateComponents.surfaceId is required",
		},
		{
			name: "updateComponents missing root",
			msg: &Message{
				UpdateComponents: &UpdateComponents{
					SurfaceID:  "s",
					Components: []Component{{ID: "not-root"}},
				},
			},
			wantErr: "id='root'",
		},
		{
			name: "updateComponents empty components",
			msg: &Message{
				UpdateComponents: &UpdateComponents{SurfaceID: "s"},
			},
			wantErr: "at least one component",
		},
		{
			name: "updateDataModel missing surfaceId",
			msg: &Message{
				UpdateDataModel: &UpdateDataModel{Path: "/"},
			},
			wantErr: "updateDataModel.surfaceId is required",
		},
		{
			name: "two top-level fields",
			msg: &Message{
				CreateSurface:    &CreateSurface{SurfaceID: "s", CatalogID: "c"},
				UpdateComponents: &UpdateComponents{SurfaceID: "s", Components: []Component{{ID: "root"}}},
			},
			wantErr: "exactly one top-level field",
		},
		{
			name: "zero top-level fields",
			msg:  &Message{},
			wantErr: "exactly one top-level field",
		},
		{
			name: "valid createSurface",
			msg: &Message{
				CreateSurface: &CreateSurface{SurfaceID: "s", CatalogID: "c"},
			},
			wantErr: "",
		},
		{
			name: "valid updateComponents",
			msg: &Message{
				UpdateComponents: &UpdateComponents{
					SurfaceID:  "s",
					Components: []Component{{ID: "root"}},
				},
			},
			wantErr: "",
		},
		{
			name: "valid updateDataModel",
			msg: &Message{
				UpdateDataModel: &UpdateDataModel{SurfaceID: "s"},
			},
			wantErr: "",
		},
		{
			name: "valid deleteSurface",
			msg: &Message{
				DeleteSurface: &DeleteSurface{SurfaceID: "s"},
			},
			wantErr: "",
		},
		{
			name: "valid callFunction",
			msg: &Message{
				CallFunction: &CallFunction{FunctionCallID: "f"},
			},
			wantErr: "",
		},
		{
			name: "valid actionResponse",
			msg: &Message{
				ActionResponse: &ActionResponse{ActionID: "a"},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMessage(tt.msg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestHandleA2UIToolCall(t *testing.T) {
	tt := Tool()
	if tt == nil {
		t.Fatal("Tool() returned nil")
	}
	if tt.Spec.Name != ToolName {
		t.Errorf("name = %q, want %q", tt.Spec.Name, ToolName)
	}
	if !tt.Spec.AlwaysLoad {
		t.Errorf("expected AlwaysLoad")
	}
	if tt.Spec.Group != tool.ToolGroupCore {
		t.Errorf("group = %v, want %v", tt.Spec.Group, tool.ToolGroupCore)
	}

	// Test handler with missing arg
	ctx := &tool.ToolContext{State: map[string]any{}}
	res, err := tt.Handler(ctx, map[string]any{})
	if err == nil {
		t.Fatalf("expected error for missing arg")
	}
	m, ok := res.(map[string]any)
	if !ok || m[ToolErrorKey] == nil {
		t.Errorf("expected error key in result")
	}

	// Test handler with valid JSON
	res, err = tt.Handler(ctx, map[string]any{
		"a2ui_json": `{"createSurface": {"surfaceId": "s1", "catalogId": "c1"}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok = res.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", res)
	}
	if m[ValidatedA2UIJSONKey] == nil {
		t.Errorf("expected validated key")
	}
	if ctx.State["a2ui_surface_s1"] == nil {
		t.Errorf("expected state key a2ui_surface_s1")
	}
}

func TestHandleA2UIToolCallWithFix(t *testing.T) {
	ctx := &tool.ToolContext{State: map[string]any{}}
	res, err := Tool().Handler(ctx, map[string]any{
		"a2ui_json": `{surfaceId: "s1", catalogId: "c1", createSurface: {}}`, // invalid, should fail
	})
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
	m, ok := res.(map[string]any)
	if !ok || m[ToolErrorKey] == nil {
		t.Errorf("expected error key in result")
	}
}

func TestToolDescription(t *testing.T) {
	if !strings.Contains(toolDescription(), SchemaBlockStart) {
		t.Errorf("expected schema block start in description")
	}
}
