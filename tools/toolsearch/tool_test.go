package toolsearch

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

// mockDiscovery implements Discovery for testing.
type mockDiscovery struct {
	tools        []*tool.Tool
	discovered   map[string]bool
	discoverLog  []string
}

func newMockDiscovery() *mockDiscovery {
	return &mockDiscovery{
		discovered: make(map[string]bool),
	}
}

func (m *mockDiscovery) GetAllExtendedTools() []*tool.Tool {
	return m.tools
}

func (m *mockDiscovery) GetAllCoreTools() []*tool.Tool {
	return nil
}

func (m *mockDiscovery) DiscoverTool(tapeName, toolName string) {
	m.discovered[toolName] = true
	m.discoverLog = append(m.discoverLog, toolName)
}

func (m *mockDiscovery) IsToolDiscovered(tapeName, toolName string) bool {
	return m.discovered[toolName]
}

func TestNewTool(t *testing.T) {
	d := newMockDiscovery()
	tt := NewTool(d)
	if tt == nil {
		t.Fatal("NewTool returned nil")
	}
	if tt.Spec.Name != "toolSearch" {
		t.Errorf("name = %q", tt.Spec.Name)
	}
	if tt.Spec.Group != tool.ToolGroupCore {
		t.Errorf("group = %v", tt.Spec.Group)
	}
	if !tt.Spec.AlwaysLoad {
		t.Errorf("expected AlwaysLoad")
	}
	if tt.Handler == nil {
		t.Errorf("handler nil")
	}
}

func TestHandleToolSearchEmptyQuery(t *testing.T) {
	d := newMockDiscovery()
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: "test-tape"}
	res, err := tt.Handler(ctx, map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil result, got %v", res)
	}
}

func TestHandleToolSearchNoTape(t *testing.T) {
	d := newMockDiscovery()
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: ""}
	res, err := tt.Handler(ctx, map[string]any{"query": "search"})
	if err == nil {
		t.Fatal("expected error for empty tape")
	}
	if !strings.Contains(err.Error(), "tape name not available") {
		t.Errorf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil result, got %v", res)
	}
}

func TestHandleToolSearchNoExtendedTools(t *testing.T) {
	d := newMockDiscovery()
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: "test-tape"}
	res, err := tt.Handler(ctx, map[string]any{"query": "search"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.(string), "No additional tools available") {
		t.Errorf("expected no tools message, got %v", res)
	}
}

func TestHandleToolSearchKeywordMatch(t *testing.T) {
	d := newMockDiscovery()
	d.tools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "webSearch", Description: "search the web"}},
		{Spec: tool.ToolSpec{Name: "fileRead", Description: "read a file"}},
	}
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: "test-tape"}
	res, err := tt.Handler(ctx, map[string]any{"query": "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(str, "webSearch") {
		t.Errorf("expected webSearch in result, got %s", str)
	}
	if !d.discovered["webSearch"] {
		t.Errorf("webSearch should be discovered on first call")
	}
	// Search again - should report already discovered
	res2, err := tt.Handler(ctx, map[string]any{"query": "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str2 := res2.(string)
	if !strings.Contains(str2, "already discovered") {
		t.Errorf("expected already discovered message, got %s", str2)
	}
	if !d.discovered["webSearch"] {
		t.Errorf("webSearch should be discovered after second call")
	}
}

func TestHandleToolSearchSelect(t *testing.T) {
	d := newMockDiscovery()
	d.tools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "webSearch", Description: "search the web"}},
		{Spec: tool.ToolSpec{Name: "fileRead", Description: "read a file"}},
	}
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: "test-tape"}
	res, err := tt.Handler(ctx, map[string]any{"query": "select:webSearch,fileRead"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(str, "webSearch") || !strings.Contains(str, "fileRead") {
		t.Errorf("expected both tools in result, got %s", str)
	}
	if !strings.Contains(str, "Selected") {
		t.Errorf("expected 'Selected' in result, got %s", str)
	}
	if !d.discovered["webSearch"] || !d.discovered["fileRead"] {
		t.Errorf("expected tools to be discovered")
	}
}

func TestHandleToolSearchSelectPartialMissing(t *testing.T) {
	d := newMockDiscovery()
	d.tools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "webSearch", Description: "search the web"}},
	}
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: "test-tape"}
	res, err := tt.Handler(ctx, map[string]any{"query": "select:webSearch,nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(str, "webSearch") {
		t.Errorf("expected webSearch in result, got %s", str)
	}
	if !strings.Contains(str, "nonexistent") {
		t.Errorf("expected nonexistent in warning, got %s", str)
	}
	if !strings.Contains(str, "Warning") {
		t.Errorf("expected Warning in result, got %s", str)
	}
}

func TestHandleToolSearchSelectDuplicate(t *testing.T) {
	d := newMockDiscovery()
	d.tools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "webSearch", Description: "search the web"}},
	}
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: "test-tape"}
	// First select
	res, err := tt.Handler(ctx, map[string]any{"query": "select:webSearch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.(string), "Selected") {
		t.Errorf("expected Selected on first select")
	}
	// Second select of same tool
	res2, err := tt.Handler(ctx, map[string]any{"query": "select:webSearch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res2.(string), "Selected") {
		t.Errorf("expected Selected even on duplicate, got %s", res2.(string))
	}
}

func TestHandleToolSearchNoMatch(t *testing.T) {
	d := newMockDiscovery()
	d.tools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "webSearch", Description: "search the web"}},
	}
	tt := NewTool(d)
	ctx := &tool.ToolContext{Tape: "test-tape"}
	res, err := tt.Handler(ctx, map[string]any{"query": "something unrelated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	str := res.(string)
	if !strings.Contains(str, "No tools found") {
		t.Errorf("expected no match message, got %s", str)
	}
}
