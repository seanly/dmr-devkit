package mcp

import (
	"strings"
	"testing"

	mcpproto "github.com/mark3labs/mcp-go/mcp"
	"github.com/seanly/dmr-devkit/tool"
)

func TestBuildEnv(t *testing.T) {
	if got := buildEnv(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
	if got := buildEnv(map[string]string{}); got != nil {
		t.Errorf("expected nil for empty map, got %v", got)
	}
	env := buildEnv(map[string]string{"FOO": "bar", "BAZ": "qux"})
	if len(env) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(env))
	}
	found := 0
	for _, e := range env {
		if e == "FOO=bar" || e == "BAZ=qux" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("missing entries: %v", env)
	}
}

func TestExtractText(t *testing.T) {
	if got := extractText(nil); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
	res := &mcpproto.CallToolResult{
		Content: []mcpproto.Content{
			mcpproto.TextContent{Text: "hello"},
			mcpproto.TextContent{Text: "world"},
		},
	}
	if got := extractText(res); got != "hello\nworld" {
		t.Errorf("got %q", got)
	}
	res2 := &mcpproto.CallToolResult{
		Content: []mcpproto.Content{
			mcpproto.ImageContent{Type: "image", Data: "data", MIMEType: "image/png"},
		},
	}
	if got := extractText(res2); got != "" {
		t.Errorf("expected empty for non-text, got %q", got)
	}
}

func TestInputSchemaToMap(t *testing.T) {
	// RawInputSchema takes priority
	raw := []byte(`{"type":"object","properties":{"x":{"type":"string"}}}`)
	mt := mcpproto.Tool{RawInputSchema: raw}
	m := inputSchemaToMap(mt)
	if m["type"] != "object" {
		t.Errorf("got %v", m)
	}

	// Structured InputSchema fallback
	mt2 := mcpproto.Tool{
		InputSchema: mcpproto.ToolInputSchema{
			Properties: map[string]any{"y": "number"},
			Required:   []string{"y"},
		},
	}
	m2 := inputSchemaToMap(mt2)
	if m2["type"] != "object" {
		t.Errorf("got %v", m2)
	}
	props, ok := m2["properties"].(map[string]any)
	if !ok || props["y"] != "number" {
		t.Errorf("got properties %v", m2["properties"])
	}
	req, ok := m2["required"].([]string)
	if !ok || len(req) != 1 || req[0] != "y" {
		t.Errorf("got required %v", m2["required"])
	}

	// Empty InputSchema defaults
	mt3 := mcpproto.Tool{}
	m3 := inputSchemaToMap(mt3)
	if m3["type"] != "object" {
		t.Errorf("got %v", m3)
	}
	emptyProps, ok := m3["properties"].(map[string]any)
	if !ok || len(emptyProps) != 0 {
		t.Errorf("expected empty properties, got %v", m3["properties"])
	}
}

func TestBridgeTool(t *testing.T) {
	mt := mcpproto.Tool{
		Name:        "testTool",
		Description: "does something",
		RawInputSchema: []byte(`{"type":"object","properties":{"x":{"type":"string"}}}`),
	}
	conn := &Conn{Name: "testServer"}
	tt := BridgeTool("testServer", conn, mt)
	if tt.Spec.Name != "mcp_testServer_testTool" {
		t.Errorf("name = %q", tt.Spec.Name)
	}
	if tt.Spec.Description != "does something" {
		t.Errorf("description = %q", tt.Spec.Description)
	}
	if tt.Spec.Group != tool.ToolGroupMCP {
		t.Errorf("group = %v", tt.Spec.Group)
	}
	if !strings.Contains(tt.Spec.SearchHint, "mcp") {
		t.Errorf("search hint = %q", tt.Spec.SearchHint)
	}
	if tt.Handler == nil {
		t.Errorf("handler nil")
	}
}

func TestBridgeToolSearchHint(t *testing.T) {
	// Empty description falls back to name
	mt := mcpproto.Tool{Name: "doIt"}
	conn := &Conn{Name: "srv"}
	tt := BridgeTool("srv", conn, mt)
	if !strings.Contains(tt.Spec.SearchHint, "doIt") {
		t.Errorf("search hint = %q", tt.Spec.SearchHint)
	}

	// Without server name
	tt2 := BridgeTool("", conn, mcpproto.Tool{Name: "x"})
	if tt2.Spec.SearchHint != "" {
		t.Errorf("expected empty search hint, got %q", tt2.Spec.SearchHint)
	}
}

func TestBridgeTools(t *testing.T) {
	conn := &Conn{}
	mts := []mcpproto.Tool{
		{Name: "a"},
		{Name: "b"},
	}
	conn.tools = mts
	 tools := BridgeTools("srv", conn)
	 if len(tools) != 2 {
		 t.Fatalf("expected 2 tools, got %d", len(tools))
	 }
	 if tools[0].Spec.Name != "mcp_srv_a" {
		 t.Errorf("name = %q", tools[0].Spec.Name)
	 }
	 if tools[1].Spec.Name != "mcp_srv_b" {
		 t.Errorf("name = %q", tools[1].Spec.Name)
	 }
}

func TestServerConfigDefaults(t *testing.T) {
	sc := ServerConfig{Name: "test"}
	if sc.Name != "test" {
		t.Errorf("Name = %q", sc.Name)
	}
	// Just a struct definition test; no real logic here.
	sc.Transport = "stdio"
	if sc.Transport != "stdio" {
		t.Errorf("Transport = %q", sc.Transport)
	}
}
