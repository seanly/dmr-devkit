package tool

import (
	"errors"
	"testing"
)

func TestTool_GetGroup(t *testing.T) {
	tests := []struct {
		name     string
		group    ToolGroup
		expected ToolGroup
	}{
		{"explicit core", ToolGroupCore, ToolGroupCore},
		{"explicit extended", ToolGroupExtended, ToolGroupExtended},
		{"explicit mcp", ToolGroupMCP, ToolGroupMCP},
		{"empty defaults to core", "", ToolGroupCore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &Tool{Spec: ToolSpec{Group: tt.group}}
			if got := tool.GetGroup(); got != tt.expected {
				t.Errorf("GetGroup() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTool_IsCore(t *testing.T) {
	tests := []struct {
		name       string
		group      ToolGroup
		alwaysLoad bool
		expected   bool
	}{
		{"core group", ToolGroupCore, false, true},
		{"extended group", ToolGroupExtended, false, false},
		{"mcp group", ToolGroupMCP, false, false},
		{"extended with alwaysLoad", ToolGroupExtended, true, true},
		{"mcp with alwaysLoad", ToolGroupMCP, true, true},
		{"empty group (core)", "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &Tool{Spec: ToolSpec{Group: tt.group, AlwaysLoad: tt.alwaysLoad}}
			if got := tool.IsCore(); got != tt.expected {
				t.Errorf("IsCore() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTool_IsDeferred(t *testing.T) {
	tests := []struct {
		name       string
		group      ToolGroup
		alwaysLoad bool
		expected   bool
	}{
		{"core group", ToolGroupCore, false, false},
		{"extended group", ToolGroupExtended, false, true},
		{"mcp group", ToolGroupMCP, false, true},
		{"extended with alwaysLoad", ToolGroupExtended, true, false},
		{"empty group (core)", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &Tool{Spec: ToolSpec{Group: tt.group, AlwaysLoad: tt.alwaysLoad}}
			if got := tool.IsDeferred(); got != tt.expected {
				t.Errorf("IsDeferred() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTool_GetDescription(t *testing.T) {
	t.Run("static description", func(t *testing.T) {
		tool := &Tool{Spec: ToolSpec{Description: "static desc"}}
		if got := tool.GetDescription(nil); got != "static desc" {
			t.Errorf("GetDescription() = %v, want static desc", got)
		}
	})

	t.Run("dynamic description success", func(t *testing.T) {
		tool := &Tool{
			Spec: ToolSpec{Description: "static"},
			DynamicDescription: func(ctx *ToolContext) (string, error) {
				return "dynamic", nil
			},
		}
		ctx := &ToolContext{}
		if got := tool.GetDescription(ctx); got != "dynamic" {
			t.Errorf("GetDescription() = %v, want dynamic", got)
		}
	})

	t.Run("dynamic description error falls back", func(t *testing.T) {
		tool := &Tool{
			Spec: ToolSpec{Description: "static fallback"},
			DynamicDescription: func(ctx *ToolContext) (string, error) {
				return "", errors.New("dynamic error")
			},
		}
		ctx := &ToolContext{}
		if got := tool.GetDescription(ctx); got != "static fallback" {
			t.Errorf("GetDescription() = %v, want static fallback", got)
		}
	})

	t.Run("dynamic description nil ctx falls back", func(t *testing.T) {
		tool := &Tool{
			Spec: ToolSpec{Description: "static"},
			DynamicDescription: func(ctx *ToolContext) (string, error) {
				return "dynamic", nil
			},
		}
		if got := tool.GetDescription(nil); got != "static" {
			t.Errorf("GetDescription() = %v, want static", got)
		}
	})
}

func TestFilterCoreTools(t *testing.T) {
	tools := []*Tool{
		{Spec: ToolSpec{Name: "core1", Group: ToolGroupCore}},
		{Spec: ToolSpec{Name: "extended1", Group: ToolGroupExtended}},
		{Spec: ToolSpec{Name: "core2", Group: ""}},                                      // defaults to core
		{Spec: ToolSpec{Name: "extended2", Group: ToolGroupExtended, AlwaysLoad: true}}, // forced core
	}

	core := FilterCoreTools(tools)
	if len(core) != 3 {
		t.Errorf("FilterCoreTools returned %d tools, want 3", len(core))
	}

	// Check that all returned tools are core
	for _, tool := range core {
		if !tool.IsCore() {
			t.Errorf("Tool %s should be core", tool.Spec.Name)
		}
	}
}

func TestFilterDeferredTools(t *testing.T) {
	tools := []*Tool{
		{Spec: ToolSpec{Name: "core1", Group: ToolGroupCore}},
		{Spec: ToolSpec{Name: "extended1", Group: ToolGroupExtended}},
		{Spec: ToolSpec{Name: "mcp1", Group: ToolGroupMCP}},
		{Spec: ToolSpec{Name: "forcedCore", Group: ToolGroupExtended, AlwaysLoad: true}},
	}

	deferred := FilterDeferredTools(tools)
	if len(deferred) != 2 {
		t.Errorf("FilterDeferredTools returned %d tools, want 2", len(deferred))
	}

	// Check names
	names := make(map[string]bool)
	for _, tool := range deferred {
		names[tool.Spec.Name] = true
	}
	if !names["extended1"] || !names["mcp1"] {
		t.Errorf("Expected extended1 and mcp1, got %v", names)
	}
}

func TestSearchTools(t *testing.T) {
	tools := []*Tool{
		{Spec: ToolSpec{Name: "shell", Description: "Execute commands", SearchHint: "bash sh terminal"}},
		{Spec: ToolSpec{Name: "webFetch", Description: "Fetch web pages", SearchHint: "http curl download"}},
		{Spec: ToolSpec{Name: "fsRead", Description: "Read files", SearchHint: "file cat read"}},
	}

	tests := []struct {
		query         string
		expectedCount int
		expectedNames []string
	}{
		{"shell", 1, []string{"shell"}},
		{"bash", 1, []string{"shell"}},
		{"web", 1, []string{"webFetch"}},
		{"http", 1, []string{"webFetch"}},
		{"file", 1, []string{"fsRead"}},
		{"read", 1, []string{"fsRead"}},
		{"", 0, nil}, // empty query returns nil
		{"xyz", 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			results := SearchTools(tools, tt.query)
			if len(results) != tt.expectedCount {
				t.Errorf("SearchTools returned %d results, want %d", len(results), tt.expectedCount)
			}
			for i, name := range tt.expectedNames {
				if i < len(results) && results[i].Spec.Name != name {
					t.Errorf("Result %d = %s, want %s", i, results[i].Spec.Name, name)
				}
			}
		})
	}
}

func TestSearchToolsScoring(t *testing.T) {
	tools := []*Tool{
		{Spec: ToolSpec{Name: "webFetch", Description: "Fetch web pages via HTTP", SearchHint: "http curl download"}},
		{Spec: ToolSpec{Name: "webSearch", Description: "Search the web for information", SearchHint: "google search query"}},
		{Spec: ToolSpec{Name: "fsRead", Description: "Read files from disk", SearchHint: "file cat read"}},
	}

	// "web search" should rank webSearch (2 words match) above webFetch (1 word match)
	results := SearchTools(tools, "web search")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Spec.Name != "webSearch" {
		t.Errorf("expected webSearch first (matches both words), got %s", results[0].Spec.Name)
	}
	if results[1].Spec.Name != "webFetch" {
		t.Errorf("expected webFetch second (matches 'web' only), got %s", results[1].Spec.Name)
	}

	// "read file" should only match fsRead
	results = SearchTools(tools, "read file")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Spec.Name != "fsRead" {
		t.Errorf("expected fsRead, got %s", results[0].Spec.Name)
	}
}
