package a2ui

import (
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

func TestHandleA2UIToolCallStoreState(t *testing.T) {
	ctx := &tool.ToolContext{State: map[string]any{}}
	_, err := handleA2UIToolCall(ctx, map[string]any{
		"a2ui_json": `{"updateComponents": {"surfaceId": "s2", "components": [{"id": "root", "component": "Text"}]}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.State["a2ui_surface_s2"] == nil {
		t.Errorf("expected state key a2ui_surface_s2")
	}
}

func TestHandleA2UIToolCallUpdateDataModelState(t *testing.T) {
	ctx := &tool.ToolContext{State: map[string]any{}}
	_, err := handleA2UIToolCall(ctx, map[string]any{
		"a2ui_json": `{"updateDataModel": {"surfaceId": "s3", "path": "/data", "value": 1}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.State["a2ui_surface_s3"] == nil {
		t.Errorf("expected state key a2ui_surface_s3")
	}
}

func TestHandleA2UIToolCallNilContext(t *testing.T) {
	res, err := handleA2UIToolCall(nil, map[string]any{
		"a2ui_json": `{"createSurface": {"surfaceId": "s4", "catalogId": "c4"}}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok || m[ValidatedA2UIJSONKey] == nil {
		t.Errorf("expected validated result")
	}
}

func TestHandleA2UIToolCallInvalidJSON(t *testing.T) {
	res, err := handleA2UIToolCall(nil, map[string]any{
		"a2ui_json": `this is not json`,
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	m, ok := res.(map[string]any)
	if !ok || m[ToolErrorKey] == nil {
		t.Errorf("expected error key in result")
	}
}

func TestHandleA2UIToolCallMissingArgType(t *testing.T) {
	res, err := handleA2UIToolCall(nil, map[string]any{
		"a2ui_json": 123, // wrong type
	})
	if err == nil {
		t.Fatal("expected error")
	}
	m, ok := res.(map[string]any)
	if !ok || m[ToolErrorKey] == nil {
		t.Errorf("expected error key")
	}
}

func TestValidateMessageVersionDefault(t *testing.T) {
	msg := &Message{CreateSurface: &CreateSurface{SurfaceID: "s", CatalogID: "c"}}
	if err := validateMessage(msg); err != nil {
		t.Fatal(err)
	}
	if msg.Version != Version {
		t.Errorf("Version = %q, want %q", msg.Version, Version)
	}
}
