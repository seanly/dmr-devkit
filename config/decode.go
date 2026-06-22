package config

import (
	"fmt"
	"reflect"

	"github.com/pelletier/go-toml/v2"
)

// UnmarshalDocument decodes a TOML document into v.
// It normalizes agent.system_prompt (string or file path array) before typed decode.
func UnmarshalDocument(data []byte, v any) error {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return err
	}

	extracted, err := extractSystemPromptFields(doc)
	if err != nil {
		return err
	}

	cleaned, err := toml.Marshal(doc)
	if err != nil {
		return err
	}

	if err := toml.Unmarshal(cleaned, v); err != nil {
		return err
	}

	return applyExtractedSystemPrompts(v, extracted)
}

type extractedSystemPrompts struct {
	global *SystemPromptValue
	perTape []SystemPromptValue
}

func extractSystemPromptFields(doc map[string]any) (extractedSystemPrompts, error) {
	var out extractedSystemPrompts
	agent, ok := doc["agent"].(map[string]any)
	if !ok {
		return out, nil
	}

	if raw, ok := agent["system_prompt"]; ok {
		sp, err := systemPromptFromAny(raw)
		if err != nil {
			return out, fmt.Errorf("agent.system_prompt: %w", err)
		}
		out.global = &sp
		delete(agent, "system_prompt")
	}

	if rawEntries, ok := agent["system_prompts"]; ok {
		entries, err := asMapSlice(rawEntries)
		if err != nil {
			return out, fmt.Errorf("agent.system_prompts: %w", err)
		}
		out.perTape = make([]SystemPromptValue, len(entries))
		for i, entry := range entries {
			raw, ok := entry["system_prompt"]
			if !ok {
				continue
			}
			sp, err := systemPromptFromAny(raw)
			if err != nil {
				return out, fmt.Errorf("agent.system_prompts[%d].system_prompt: %w", i, err)
			}
			out.perTape[i] = sp
			delete(entry, "system_prompt")
		}
	}

	return out, nil
}

func systemPromptFromAny(raw any) (SystemPromptValue, error) {
	switch val := raw.(type) {
	case string:
		return SystemPromptValue{Raw: val}, nil
	case []any:
		return systemPromptFromStringSlice(val)
	default:
		return SystemPromptValue{}, fmt.Errorf("unsupported type %T", raw)
	}
}

func systemPromptFromStringSlice(val []any) (SystemPromptValue, error) {
	files := make([]string, 0, len(val))
	for _, item := range val {
		s, ok := item.(string)
		if !ok {
			return SystemPromptValue{}, fmt.Errorf("file list must contain strings")
		}
		files = append(files, s)
	}
	return SystemPromptValue{Files: files}, nil
}

func asMapSlice(raw any) ([]map[string]any, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array of tables, got %T", raw)
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entry %d: expected table, got %T", i, item)
		}
		out[i] = m
	}
	return out, nil
}

func applyExtractedSystemPrompts(v any, ex extractedSystemPrompts) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("decode target must be a non-nil pointer")
	}
	rv = rv.Elem()

	agentField := rv.FieldByName("Agent")
	if !agentField.IsValid() || !agentField.CanSet() {
		return nil
	}

	if ex.global != nil {
		spField := agentField.FieldByName("SystemPrompt")
		if spField.IsValid() && spField.CanSet() {
			spField.Set(reflect.ValueOf(*ex.global))
		}
	}

	if len(ex.perTape) == 0 {
		return nil
	}

	spEntriesField := agentField.FieldByName("SystemPrompts")
	if !spEntriesField.IsValid() || spEntriesField.Kind() != reflect.Slice {
		return nil
	}
	for i := 0; i < spEntriesField.Len() && i < len(ex.perTape); i++ {
		entry := spEntriesField.Index(i)
		if !entry.IsValid() {
			continue
		}
		spField := entry.FieldByName("SystemPrompt")
		if spField.IsValid() && spField.CanSet() && (ex.perTape[i].Raw != "" || len(ex.perTape[i].Files) > 0) {
			spField.Set(reflect.ValueOf(ex.perTape[i]))
		}
	}
	return nil
}
