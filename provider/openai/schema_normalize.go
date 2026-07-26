package openai

// normalizeToolParams deep-copies a tool parameters JSON Schema and rewrites
// union nodes into a shape accepted by strict backends (e.g. Moonshot) while
// remaining valid for OpenAI-compatible providers:
// when a node has both type and anyOf/oneOf, type is moved into each branch.
func normalizeToolParams(params any) any {
	if params == nil {
		return nil
	}
	return normalizeToolSchema(params)
}

func normalizeToolSchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeToolSchema(val)
		}
		pushTypeIntoUnionBranches(out)
		return out
	case []any:
		out := make([]any, len(x))
		for i, el := range x {
			out[i] = normalizeToolSchema(el)
		}
		return out
	default:
		return v
	}
}

func pushTypeIntoUnionBranches(schema map[string]any) {
	for _, key := range []string{"anyOf", "oneOf"} {
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
				} else if shouldDefaultUnionItemToObject(schema, m) {
					m["type"] = "object"
				}
			}
			if hasUnion(m) {
				pushTypeIntoUnionBranches(m)
			}
		}
	}
}

func hasUnion(schema map[string]any) bool {
	if _, ok := schema["anyOf"]; ok {
		return true
	}
	_, ok := schema["oneOf"]
	return ok
}

// shouldDefaultUnionItemToObject covers schemas like vsphere's
// {"type":"object","properties":...,"anyOf":[{"required":["name"]},...]}
// after the parent type has already been stripped, or when the parent never
// had type but clearly describes an object.
func shouldDefaultUnionItemToObject(parent, item map[string]any) bool {
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
