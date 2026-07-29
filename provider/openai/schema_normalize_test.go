package openai

import (
	"encoding/json"
	"testing"
)

func TestNormalizeToolParams_VsphereStyleRootAnyOf(t *testing.T) {
	// Mirrors dmr-plugin-vsphere: type on parent + anyOf of required-only branches.
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
		t.Fatalf("parent type should remain object, got %#v", out["type"])
	}
	items, ok := out["anyOf"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("anyOf = %#v", out["anyOf"])
	}
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item[%d] not map", i)
		}
		if typ, _ := m["type"].(string); typ != "object" {
			t.Fatalf("item[%d].type = %v, want object", i, m["type"])
		}
	}
	if _, has := in["type"]; !has {
		t.Fatal("input schema was mutated")
	}
	if _, has := in["anyOf"].([]any)[0].(map[string]any)["type"]; has {
		t.Fatal("input anyOf item was mutated")
	}
}

func TestNormalizeToolParams_NestedAnyOf(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"anyOf": []any{
			map[string]any{
				"type": "object",
				"anyOf": []any{
					map[string]any{"required": []any{"a"}},
					map[string]any{"required": []any{"b"}},
				},
			},
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"c": map[string]any{"type": "string"}},
			},
		},
	}
	out := normalizeToolParams(in).(map[string]any)
	if typ, _ := out["type"].(string); typ != "object" {
		t.Fatalf("root type should remain object, got %v", out["type"])
	}
	outer := out["anyOf"].([]any)
	nested := outer[0].(map[string]any)
	if typ, _ := nested["type"].(string); typ != "object" {
		t.Fatalf("middle type should remain object, got %v", nested["type"])
	}
	inner := nested["anyOf"].([]any)
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
}

func TestBuildRequest_NormalizesToolsForAllProviders(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"anyOf": []any{
			map[string]any{"required": []any{"name"}},
		},
	}
	tools := []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":       "vsphereFindVM",
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
				t.Fatalf("expected parent type object, got %#v", got["type"])
			}
			item := got["anyOf"].([]any)[0].(map[string]any)
			if item["type"] != "object" {
				t.Fatalf("branch type = %v", item["type"])
			}
		})
	}

	if params["type"] != "object" {
		t.Fatal("original parameters mutated")
	}
}
