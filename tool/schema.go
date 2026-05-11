package tool

import (
	"fmt"
	"math"
	"strings"

	"github.com/seanly/dmr-devkit/core"
)

// validateParameters checks a subset of JSON Schema (OpenAI-style tool parameters)
// against args: required keys, property types, and enum when present.
// It is intentionally conservative: unknown or partial schemas are skipped.
func validateParameters(t *Tool, args map[string]any) *core.ErrorPayload {
	if t == nil || t.Spec.Parameters == nil {
		return nil
	}
	schema := t.Spec.Parameters
	typ, _ := schema["type"].(string)
	if typ != "" && typ != "object" {
		return nil
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return nil
	}

	required := normalizeStringSlice(schema["required"])

	for _, name := range required {
		v, present := args[name]
		if !present || v == nil {
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: missing required argument %q", t.Spec.Name, name),
			}
		}
		prop, _ := props[name].(map[string]any)
		if isEmptyRequiredValue(prop, v) {
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: required argument %q must not be empty", t.Spec.Name, name),
			}
		}
	}

	for name, v := range args {
		prop, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		if v == nil {
			continue
		}
		if err := validatePropertyValue(t.Spec.Name, name, prop, v); err != nil {
			return err
		}
	}

	return nil
}

func normalizeStringSlice(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func isEmptyRequiredValue(prop map[string]any, v any) bool {
	pt, _ := prop["type"].(string)
	switch pt {
	case "string":
		s, ok := v.(string)
		return !ok || strings.TrimSpace(s) == ""
	case "array":
		arr, ok := v.([]any)
		return !ok || len(arr) == 0
	case "object":
		obj, ok := v.(map[string]any)
		return !ok || len(obj) == 0
	default:
		return false
	}
}

func validatePropertyValue(toolName, propName string, prop map[string]any, v any) *core.ErrorPayload {
	pt, _ := prop["type"].(string)
	switch pt {
	case "string":
		if _, ok := v.(string); !ok {
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: argument %q must be a string", toolName, propName),
			}
		}
		if err := validateEnum(toolName, propName, prop, v); err != nil {
			return err
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: argument %q must be a boolean", toolName, propName),
			}
		}
	case "number":
		switch v.(type) {
		case float64, int, int32, int64, uint, uint32, uint64:
			// ok
		default:
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: argument %q must be a number", toolName, propName),
			}
		}
	case "integer":
		switch n := v.(type) {
		case float64:
			if math.Trunc(n) != n || math.IsNaN(n) || math.IsInf(n, 0) {
				return &core.ErrorPayload{
					Kind:    core.ErrInvalidInput,
					Message: fmt.Sprintf("tool %q: argument %q must be an integer", toolName, propName),
				}
			}
		case int, int32, int64, uint, uint32, uint64:
		default:
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: argument %q must be an integer", toolName, propName),
			}
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: argument %q must be an object", toolName, propName),
			}
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q: argument %q must be an array", toolName, propName),
			}
		}
		if itemSchema, ok := prop["items"].(map[string]any); ok {
			it, _ := itemSchema["type"].(string)
			for i, el := range arr {
				switch it {
				case "string":
					if _, ok := el.(string); !ok {
						return &core.ErrorPayload{
							Kind:    core.ErrInvalidInput,
							Message: fmt.Sprintf("tool %q: argument %q[%d] must be a string", toolName, propName, i),
						}
					}
				case "object":
					if _, ok := el.(map[string]any); !ok {
						return &core.ErrorPayload{
							Kind:    core.ErrInvalidInput,
							Message: fmt.Sprintf("tool %q: argument %q[%d] must be an object", toolName, propName, i),
						}
					}
				case "integer", "number":
					switch el.(type) {
					case float64, int, int32, int64:
					default:
						return &core.ErrorPayload{
							Kind:    core.ErrInvalidInput,
							Message: fmt.Sprintf("tool %q: argument %q[%d] must be a number", toolName, propName, i),
						}
					}
				}
			}
		}
	}
	return nil
}

func validateEnum(toolName, propName string, prop map[string]any, v any) *core.ErrorPayload {
	raw, ok := prop["enum"]
	if !ok || raw == nil {
		return nil
	}
	allowed := normalizeStringSlice(raw)
	if len(allowed) == 0 {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	for _, e := range allowed {
		if s == e {
			return nil
		}
	}
	return &core.ErrorPayload{
		Kind:    core.ErrInvalidInput,
		Message: fmt.Sprintf("tool %q: argument %q value %q is not one of allowed values", toolName, propName, s),
	}
}
