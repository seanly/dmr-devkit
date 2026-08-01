package openai

// normalizeToolParams deep-copies a tool parameters JSON Schema and rewrites it
// into the shape OpenAI-compatible function calling expects:
//
//  1. Root must be a plain object schema (type "object" + properties).
//  2. A node must not combine "type" with anyOf/oneOf — strict backends reject that.
//
// Root compositions (anyOf / oneOf / allOf) are therefore collapsed into a single
// object schema. Nested unions keep their structure; rule (2) is applied by
// moving the parent type onto each branch.
func normalizeToolParams(params any) any {
	if params == nil {
		return emptyObjectSchema()
	}
	out := walkSchema(params)
	if m, ok := out.(map[string]any); ok {
		collapseRootCompositions(m)
		ensureObjectSchema(m)
	}
	return out
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func walkSchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = walkSchema(val)
		}
		separateTypeFromUnions(out)
		return out
	case []any:
		out := make([]any, len(x))
		for i, el := range x {
			out[i] = walkSchema(el)
		}
		return out
	default:
		return v
	}
}

// JSON Schema composition keywords. Root-level ones are collapsed because the
// tool-parameters root must remain a plain object schema.
var compositionKeys = []string{"anyOf", "oneOf", "allOf"}

// Union keywords that cannot share a node with "type" on strict backends.
var unionKeys = []string{"anyOf", "oneOf"}

// collapseRootCompositions merges object-shaped branches into the root and
// removes the composition keyword. Branch-only required constraints are dropped
// (the model still sees the union of properties).
func collapseRootCompositions(schema map[string]any) {
	for _, key := range compositionKeys {
		raw, ok := schema[key]
		if !ok {
			continue
		}
		items, _ := raw.([]any)
		for _, item := range items {
			branch, ok := item.(map[string]any)
			if !ok {
				continue
			}
			mergeObjectFields(schema, branch)
		}
		delete(schema, key)
	}
}

// mergeObjectFields copies object-shape fields from src into dst.
// properties are unioned; existing dst keys win on conflict.
func mergeObjectFields(dst, src map[string]any) {
	sp, ok := src["properties"].(map[string]any)
	if !ok {
		return
	}
	dp, _ := dst["properties"].(map[string]any)
	if dp == nil {
		dp = map[string]any{}
	}
	for k, v := range sp {
		if _, exists := dp[k]; !exists {
			dp[k] = cloneJSONValue(v)
		}
	}
	dst["properties"] = dp
}

// separateTypeFromUnions enforces "type XOR anyOf/oneOf" on a schema node by
// moving the parent type onto each branch (or defaulting branches to object).
func separateTypeFromUnions(schema map[string]any) {
	for _, key := range unionKeys {
		raw, ok := schema[key]
		if !ok {
			continue
		}
		items, ok := raw.([]any)
		if !ok || len(items) == 0 {
			continue
		}

		parentType, hasParentType := schema["type"]
		if hasParentType {
			delete(schema, "type")
		}

		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if _, exists := m["type"]; !exists {
				if hasParentType {
					m["type"] = cloneJSONValue(parentType)
				} else if looksLikeObjectSchema(schema, m) {
					m["type"] = "object"
				}
			}
			if hasUnion(m) {
				separateTypeFromUnions(m)
			}
		}
	}
}

func hasUnion(schema map[string]any) bool {
	for _, key := range unionKeys {
		if _, ok := schema[key]; ok {
			return true
		}
	}
	return false
}

// ensureObjectSchema forces the tool-parameters root into a plain object schema.
func ensureObjectSchema(schema map[string]any) {
	schema["type"] = "object"
	if _, ok := schema["properties"]; !ok {
		schema["properties"] = map[string]any{}
	}
}

// looksLikeObjectSchema detects object-shaped union parents/items that omit type,
// e.g. {"properties":...,"anyOf":[{"required":["name"]},...]}.
func looksLikeObjectSchema(parent, item map[string]any) bool {
	if _, ok := parent["properties"]; ok {
		return true
	}
	if _, ok := item["required"]; ok {
		return true
	}
	if _, ok := item["properties"]; ok {
		return true
	}
	return false
}

func cloneJSONValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = cloneJSONValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, el := range x {
			out[i] = cloneJSONValue(el)
		}
		return out
	default:
		return v
	}
}
