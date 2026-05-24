package a2ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Catalog holds a loaded A2UI component catalog with its schemas.
type Catalog struct {
	CatalogID     string
	Name          string
	CatalogSchema map[string]any // the "components" object from catalog.json
	S2CSchema     map[string]any // server_to_client.json
	CommonTypes   map[string]any // common_types.json
	Examples      string         // concatenated example JSONs
}

// LoadCatalog loads a catalog from filesystem paths.
func LoadCatalog(catalogPath, s2cPath, commonTypesPath string) (*Catalog, error) {
	catalogSchema, err := loadJSON(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("a2ui: load catalog schema: %w", err)
	}
	s2cSchema, err := loadJSON(s2cPath)
	if err != nil {
		return nil, fmt.Errorf("a2ui: load s2c schema: %w", err)
	}
	commonSchema, err := loadJSON(commonTypesPath)
	if err != nil {
		return nil, fmt.Errorf("a2ui: load common types: %w", err)
	}

	catalogID, _ := catalogSchema["catalogId"].(string)
	if catalogID == "" {
		catalogID = "https://a2ui.org/specification/v0_10/catalogs/basic/catalog.json"
	}

	return &Catalog{
		CatalogID:     catalogID,
		Name:          "basic",
		CatalogSchema: catalogSchema,
		S2CSchema:     s2cSchema,
		CommonTypes:   commonSchema,
	}, nil
}

// LoadCatalogWithExamples loads a catalog plus example files from a directory.
func LoadCatalogWithExamples(catalogPath, s2cPath, commonTypesPath, examplesDir string) (*Catalog, error) {
	c, err := LoadCatalog(catalogPath, s2cPath, commonTypesPath)
	if err != nil {
		return nil, err
	}
	if examplesDir != "" {
		examples, err := loadExamples(examplesDir)
		if err != nil {
			return nil, fmt.Errorf("a2ui: load examples: %w", err)
		}
		c.Examples = examples
	}
	return c, nil
}

// MustLoadCatalog is like LoadCatalog but panics on error.
func MustLoadCatalog(catalogPath, s2cPath, commonTypesPath string) *Catalog {
	c, err := LoadCatalog(catalogPath, s2cPath, commonTypesPath)
	if err != nil {
		panic(err)
	}
	return c
}

// RenderAsLLMInstructions renders the catalog and schema as LLM instructions,
// mirroring Python A2uiCatalog.render_as_llm_instructions().
func (c *Catalog) RenderAsLLMInstructions() string {
	parts := []string{SchemaBlockStart}
	parts = append(parts, "### Server To Client Schema:\n"+mustCompactJSON(c.S2CSchema))
	if len(c.CommonTypes) > 0 {
		parts = append(parts, "### Common Types Schema:\n"+mustCompactJSON(c.CommonTypes))
	}
	parts = append(parts, "### Catalog Schema:\n"+mustCompactJSON(c.CatalogSchema))
	parts = append(parts, SchemaBlockEnd)
	return joinLines(parts)
}

// GenerateSystemPrompt assembles the final system instruction for the LLM.
// It mirrors Python A2uiSchemaManager.generate_system_prompt().
func (c *Catalog) GenerateSystemPrompt(roleDesc, workflowDesc, uiDesc string, includeSchema, includeExamples bool) string {
	parts := []string{roleDesc}
	parts = append(parts, "## Workflow Description:\n"+defaultWorkflowRules+"\n"+workflowDesc)
	if uiDesc != "" {
		parts = append(parts, "## UI Description:\n"+uiDesc)
	}
	if includeSchema {
		parts = append(parts, c.RenderAsLLMInstructions())
	}
	if includeExamples && c.Examples != "" {
		parts = append(parts, "### Examples:\n"+c.Examples)
	}
	return joinLines(parts)
}

// --- helpers ---

func loadJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func loadExamples(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var merged []string
	for _, e := range entries {
		if e.IsDir() || !hasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
		base := e.Name()[:len(e.Name())-5]
		merged = append(merged, "---BEGIN "+base+"---\n"+string(b)+"\n---END "+base+"---")
	}
	return joinLines(merged), nil
}

func mustCompactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n\n")
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

const defaultWorkflowRules = `When you need to present rich UI to the user, call the send_a2ui_json_to_client tool with valid A2UI JSON.
Follow these rules:
1. First send createSurface to create a new surface.
2. Then send updateComponents with the component tree (must include a root component).
3. Then send updateDataModel with the data.
4. Use DataBinding ({"path":"/field"}) to connect components to the data model.
5. Use action events on Buttons to handle user interactions.
6. You can send multiple updateComponents and updateDataModel messages to incrementally update the UI.`
