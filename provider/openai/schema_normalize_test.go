package openai

import (
	"encoding/json"
	"testing"
)

func TestNormalizeToolParams_RootAnyOfRequiredOnly(t *testing.T) {
	// Common pattern: shared properties + anyOf of required-only branches.
	// Root compositions are collapsed so the root stays a plain object schema.
	in := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string"},
			"guestIp": map[string]any{"type": "string", "description": "Exact guest OS IP"},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"name"}},
			map[string]any{"required": []any{"guestIp"}},
		},
	}

	out, ok := normalizeToolParams(in).(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", normalizeToolParams(in))
	}
	if typ, _ := out["type"].(string); typ != "object" {
		t.Fatalf("root type = %#v, want object", out["type"])
	}
	if _, has := out["anyOf"]; has {
		t.Fatalf("root anyOf should be collapsed, got %#v", out["anyOf"])
	}
	props, ok := out["properties"].(map[string]any)
	if !ok || props["name"] == nil || props["guestIp"] == nil {
		t.Fatalf("properties not preserved: %#v", out["properties"])
	}
	if _, has := in["anyOf"]; !has {
		t.Fatal("input schema was mutated")
	}
}

func TestNormalizeToolParams_RootAnyOfMergesBranchProperties(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"anyOf": []any{
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"a": map[string]any{"type": "string"}},
				"required":   []any{"a"},
			},
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"b": map[string]any{"type": "integer"}},
				"required":   []any{"b"},
			},
		},
	}
	out := normalizeToolParams(in).(map[string]any)
	if out["type"] != "object" {
		t.Fatalf("type = %v", out["type"])
	}
	if _, has := out["anyOf"]; has {
		t.Fatal("root anyOf should be removed")
	}
	props := out["properties"].(map[string]any)
	if props["a"] == nil || props["b"] == nil {
		t.Fatalf("merged properties = %#v", props)
	}
}

func TestNormalizeToolParams_RootAllOfMergesBranchProperties(t *testing.T) {
	in := map[string]any{
		"allOf": []any{
			map[string]any{
				"properties": map[string]any{"a": map[string]any{"type": "string"}},
			},
			map[string]any{
				"properties": map[string]any{"b": map[string]any{"type": "integer"}},
			},
		},
	}
	out := normalizeToolParams(in).(map[string]any)
	if out["type"] != "object" {
		t.Fatalf("type = %v", out["type"])
	}
	if _, has := out["allOf"]; has {
		t.Fatal("root allOf should be collapsed")
	}
	props := out["properties"].(map[string]any)
	if props["a"] == nil || props["b"] == nil {
		t.Fatalf("merged properties = %#v", props)
	}
}

func TestNormalizeToolParams_NestedAnyOf(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"choice": map[string]any{
				"type": "object",
				"anyOf": []any{
					map[string]any{"required": []any{"a"}},
					map[string]any{"required": []any{"b"}},
				},
			},
		},
	}
	out := normalizeToolParams(in).(map[string]any)
	if out["type"] != "object" {
		t.Fatalf("root type = %v", out["type"])
	}
	choice := out["properties"].(map[string]any)["choice"].(map[string]any)
	if _, has := choice["type"]; has {
		t.Fatal("nested union parent type should be stripped")
	}
	inner := choice["anyOf"].([]any)
	for i, item := range inner {
		if typ, _ := item.(map[string]any)["type"].(string); typ != "object" {
			t.Fatalf("nested item[%d].type = %v", i, item.(map[string]any)["type"])
		}
	}
}

func TestNormalizeToolParams_NilDefaultsToObject(t *testing.T) {
	out, ok := normalizeToolParams(nil).(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", normalizeToolParams(nil))
	}
	if out["type"] != "object" {
		t.Fatalf("type = %v, want object", out["type"])
	}
}

func TestNormalizeToolParams_LeavesPlainObject(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
		"required": []any{"query"},
	}
	out := normalizeToolParams(in).(map[string]any)
	raw, _ := json.Marshal(out)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["type"] != "object" {
		t.Fatalf("plain object type changed: %s", raw)
	}
	if _, ok := got["required"]; !ok {
		t.Fatalf("required dropped: %s", raw)
	}
}

func TestBuildRequest_NormalizesToolsForAllProviders(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"name"}},
		},
	}
	tools := []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":       "findResource",
				"parameters": params,
			},
		},
	}

	for _, tc := range []struct {
		name, base, model string
	}{
		{"kimi", "https://api.kimi.com/coding/v1", "kimi-code"},
		{"openai", "https://api.openai.com/v1", "gpt-4o"},
		{"deepseek", "https://api.deepseek.com", "deepseek-v4-flash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(ClientConfig{APIKey: "test", BaseURL: tc.base})
			goReq := c.buildRequest(ChatRequest{
				Model:    tc.model,
				Messages: []Message{{Role: "user", Content: "hi"}},
				Tools:    tools,
			})
			got, ok := goReq.Tools[0].Function.Parameters.(map[string]any)
			if !ok {
				t.Fatalf("parameters type %T", goReq.Tools[0].Function.Parameters)
			}
			if got["type"] != "object" {
				t.Fatalf("expected root type object, got %#v", got["type"])
			}
			if _, has := got["anyOf"]; has {
				t.Fatalf("expected root anyOf collapsed, got %#v", got)
			}
		})
	}

	if _, has := params["anyOf"]; !has {
		t.Fatal("original parameters mutated")
	}
}
