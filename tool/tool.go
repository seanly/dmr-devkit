package tool

import (
	"fmt"
	"sort"
	"strings"
)

// ToolGroup defines tool loading groups.
type ToolGroup string

const (
	// ToolGroupCore - core tools, always loaded
	ToolGroupCore ToolGroup = "core"
	// ToolGroupExtended - extended tools, loaded on demand via ToolSearch
	ToolGroupExtended ToolGroup = "extended"
	// ToolGroupMCP - MCP tools, default deferred
	ToolGroupMCP ToolGroup = "mcp"
)

// DynamicDescriptionFunc generates dynamic description based on context.
type DynamicDescriptionFunc func(ctx *ToolContext) (string, error)

// ToolSpec holds the declarative, serializable metadata for a tool.
// It contains everything needed to generate the JSON schema for LLM function calling.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema for parameters

	// === Tool Group and Loading Control ===

	// Group specifies the tool loading group.
	// "core" - always loaded on every turn
	// "extended" - deferred, needs ToolSearch to discover (default if empty)
	// "mcp" - MCP tools, default deferred
	Group ToolGroup

	// AlwaysLoad forces the tool to always be loaded regardless of Group.
	// When true, the tool is treated as core even if Group is "extended" or "mcp".
	AlwaysLoad bool

	// SearchHint provides keywords for ToolSearch matching.
	// Used when Group is "extended" or "mcp".
	// Example: "http, curl, download, html" for webFetch tool.
	SearchHint string

	// MaxResultChars controls when this tool's output is externalized to disk:
	// 0 = use model/agent default; >0 = cap in runes; -1 = never externalize (send full output).
	MaxResultChars int
}

// Tool defines a callable tool with its specification and runtime behavior.
type Tool struct {
	Spec        ToolSpec
	Handler     func(ctx *ToolContext, args map[string]any) (any, error)
	NeedContext bool

	// DynamicDescription generates description dynamically based on context.
	// If set, it overrides the static Spec.Description field.
	// Useful for including runtime info (current directory, shell type, etc.)
	DynamicDescription DynamicDescriptionFunc
}

// GetGroup returns the tool's group, defaulting to "extended" for deferred discovery.
// Only tools explicitly marked as core are loaded on every turn; unmarked tools are deferred.
func (t *Tool) GetGroup() ToolGroup {
	if t.Spec.Group == "" {
		return ToolGroupExtended
	}
	return t.Spec.Group
}

// IsCore returns true if the tool should always be loaded.
func (t *Tool) IsCore() bool {
	if t.Spec.AlwaysLoad {
		return true
	}
	return t.GetGroup() == ToolGroupCore
}

// IsDeferred returns true if the tool is deferred (needs discovery).
func (t *Tool) IsDeferred() bool {
	if t.Spec.AlwaysLoad {
		return false
	}
	group := t.GetGroup()
	return group == ToolGroupExtended || group == ToolGroupMCP
}

// GetDescription returns the tool description.
// If DynamicDescription is set and ctx is provided, it uses that; otherwise returns static Description.
func (t *Tool) GetDescription(ctx *ToolContext) string {
	if t.DynamicDescription != nil && ctx != nil {
		desc, err := t.DynamicDescription(ctx)
		if err == nil && desc != "" {
			return desc
		}
		// Fall back to static description on error
	}
	return t.Spec.Description
}

// ToSchema returns the OpenAI function tool format.
// For backward compatibility, ctx can be nil (uses static description).
func (t *Tool) ToSchema(ctx *ToolContext) map[string]any {
	fn := map[string]any{
		"name": t.Spec.Name,
	}

	// Get description (dynamic or static)
	description := t.GetDescription(ctx)
	if description != "" {
		fn["description"] = description
	}

	if t.Spec.Parameters != nil {
		fn["parameters"] = t.Spec.Parameters
	} else {
		fn["parameters"] = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	return map[string]any{
		"type":     "function",
		"function": fn,
	}
}

// ToSchemaStatic returns the static schema without context (for caching).
func (t *Tool) ToSchemaStatic() map[string]any {
	return t.ToSchema(nil)
}

// ToolSet holds both the schemas (for the API) and runnable tools (for execution).
type ToolSet struct {
	Schemas  []map[string]any
	Runnable map[string]*Tool
}

// NormalizeTools validates and organizes a slice of tools into a ToolSet.
// For backward compatibility, it uses static schemas (ctx = nil).
func NormalizeTools(tools []*Tool) (*ToolSet, error) {
	ts := &ToolSet{
		Schemas:  make([]map[string]any, 0, len(tools)),
		Runnable: make(map[string]*Tool, len(tools)),
	}

	for _, t := range tools {
		if _, exists := ts.Runnable[t.Spec.Name]; exists {
			return nil, fmt.Errorf("duplicate tool name: %q", t.Spec.Name)
		}
		// Use static schema for normalization (for caching)
		ts.Schemas = append(ts.Schemas, t.ToSchemaStatic())
		ts.Runnable[t.Spec.Name] = t
	}

	return ts, nil
}

// FilterCoreTools returns only core tools (always loaded).
func FilterCoreTools(tools []*Tool) []*Tool {
	var core []*Tool
	for _, t := range tools {
		if t.IsCore() {
			core = append(core, t)
		}
	}
	return core
}

// FilterDeferredTools returns only deferred tools (need discovery).
func FilterDeferredTools(tools []*Tool) []*Tool {
	var deferred []*Tool
	for _, t := range tools {
		if t.IsDeferred() {
			deferred = append(deferred, t)
		}
	}
	return deferred
}

// SearchTools searches for tools matching the query against name, description, and SearchHint.
// Multi-word queries use scored matching: tools matching more words rank higher.
// Tools matching ALL words come first, followed by partial matches (sorted by match count descending).
// Single-word queries behave as simple substring matches.
func SearchTools(tools []*Tool, query string) []*Tool {
	if query == "" {
		return nil
	}

	// Split query into words
	words := splitWords(query)
	if len(words) == 0 {
		return nil
	}

	type scored struct {
		tool  *Tool
		score int // number of words matched
	}

	var matches []scored
	for _, t := range tools {
		searchable := strings.ToLower(t.Spec.Name + " " + t.Spec.Description + " " + t.Spec.SearchHint)
		score := countMatchingWords(searchable, words)
		if score > 0 {
			matches = append(matches, scored{tool: t, score: score})
		}
	}

	// Sort by score descending (more matched words first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	result := make([]*Tool, len(matches))
	for i, m := range matches {
		result[i] = m.tool
	}
	return result
}

// splitWords splits a string into lowercase words (space-separated).
func splitWords(s string) []string {
	var words []string
	for _, w := range strings.Fields(s) {
		if w != "" {
			words = append(words, strings.ToLower(w))
		}
	}
	return words
}

// countMatchingWords returns how many of the given words are found in s.
func countMatchingWords(s string, words []string) int {
	count := 0
	for _, word := range words {
		if strings.Contains(s, word) {
			count++
		}
	}
	return count
}
