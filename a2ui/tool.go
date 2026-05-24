package a2ui

import (
	"encoding/json"
	"fmt"

	"github.com/seanly/dmr-devkit/tool"
)

// ToolName is the name of the A2UI tool exposed to the LLM.
const ToolName = "send_a2ui_json_to_client"

// ValidatedA2UIJSONKey is the key in the tool result containing validated A2UI JSON.
const ValidatedA2UIJSONKey = "validated_a2ui_json"

// ToolErrorKey is the key in the tool result containing error information.
const ToolErrorKey = "error"

// Tool creates the send_a2ui_json_to_client tool specification.
// The handler parses, fixes, and validates A2UI JSON from the LLM.
func Tool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        ToolName,
			Description: toolDescription(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a2ui_json": map[string]any{
						"type":        "string",
						"description": "Valid A2UI JSON Schema to send to the client.",
					},
				},
				"required": []string{"a2ui_json"},
			},
			Group:      tool.ToolGroupCore,
			AlwaysLoad: true,
		},
		Handler:     handleA2UIToolCall,
		NeedContext: true,
	}
}

func toolDescription() string {
	return "Sends A2UI JSON to the client to render rich UI for the user. " +
		"This tool can be called multiple times in the same call to render multiple UI surfaces. " +
		"The A2UI JSON Schema definition is between " +
		SchemaBlockStart + " and " + SchemaBlockEnd + " in the system instructions."
}

func handleA2UIToolCall(tc *tool.ToolContext, args map[string]any) (any, error) {
	raw, ok := args["a2ui_json"].(string)
	if !ok || raw == "" {
		return map[string]any{ToolErrorKey: "missing required arg a2ui_json"},
			fmt.Errorf("a2ui: missing required arg a2ui_json")
	}

	// 1. Auto-fix common LLM JSON issues.
	fixed, err := FixJSON(raw)
	if err != nil {
		return map[string]any{ToolErrorKey: fmt.Sprintf("failed to fix JSON: %v", err)},
			fmt.Errorf("a2ui: fix JSON: %w", err)
	}

	// 2. Parse into structured message(s).
	var msg Message
	if err := json.Unmarshal([]byte(fixed), &msg); err != nil {
		return map[string]any{ToolErrorKey: fmt.Sprintf("invalid A2UI JSON: %v", err)},
			fmt.Errorf("a2ui: unmarshal: %w", err)
	}

	// 3. Basic structural validation.
	if err := validateMessage(&msg); err != nil {
		return map[string]any{ToolErrorKey: fmt.Sprintf("validation failed: %v", err)},
			fmt.Errorf("a2ui: validate: %w", err)
	}

	// 4. Store validated payload in tool context state for downstream consumers
	// (workflow events, a2aserver conversion, etc.).
	if tc != nil {
		if tc.State == nil {
			tc.State = make(map[string]any)
		}
		key := "a2ui_message"
		if msg.CreateSurface != nil && msg.CreateSurface.SurfaceID != "" {
			key = "a2ui_surface_" + msg.CreateSurface.SurfaceID
		} else if msg.UpdateComponents != nil && msg.UpdateComponents.SurfaceID != "" {
			key = "a2ui_surface_" + msg.UpdateComponents.SurfaceID
		} else if msg.UpdateDataModel != nil && msg.UpdateDataModel.SurfaceID != "" {
			key = "a2ui_surface_" + msg.UpdateDataModel.SurfaceID
		}
		tc.State[key] = msg
	}

	return map[string]any{
		ValidatedA2UIJSONKey: msg,
		"status":             "ok",
	}, nil
}

func validateMessage(msg *Message) error {
	if msg.Version == "" {
		msg.Version = Version
	}
	nonNil := 0
	if msg.CreateSurface != nil {
		nonNil++
		if msg.CreateSurface.SurfaceID == "" {
			return fmt.Errorf("createSurface.surfaceId is required")
		}
		if msg.CreateSurface.CatalogID == "" {
			return fmt.Errorf("createSurface.catalogId is required")
		}
	}
	if msg.UpdateComponents != nil {
		nonNil++
		if msg.UpdateComponents.SurfaceID == "" {
			return fmt.Errorf("updateComponents.surfaceId is required")
		}
		if len(msg.UpdateComponents.Components) == 0 {
			return fmt.Errorf("updateComponents.components must have at least one component")
		}
		hasRoot := false
		for _, c := range msg.UpdateComponents.Components {
			if c.ID == "root" {
				hasRoot = true
				break
			}
		}
		if !hasRoot {
			return fmt.Errorf("updateComponents.components must contain a component with id='root'")
		}
	}
	if msg.UpdateDataModel != nil {
		nonNil++
		if msg.UpdateDataModel.SurfaceID == "" {
			return fmt.Errorf("updateDataModel.surfaceId is required")
		}
	}
	if msg.DeleteSurface != nil {
		nonNil++
	}
	if msg.CallFunction != nil {
		nonNil++
	}
	if msg.ActionResponse != nil {
		nonNil++
	}
	if nonNil != 1 {
		return fmt.Errorf("A2UI message must have exactly one top-level field set, got %d", nonNil)
	}
	return nil
}
