package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpproto "github.com/mark3labs/mcp-go/mcp"
	"github.com/seanly/dmr-devkit/tool"
)

// inputSchemaToMap converts MCP ToolInputSchema to map[string]any for DMR.
func inputSchemaToMap(mt mcpproto.Tool) map[string]any {
	// Try RawInputSchema first (arbitrary JSON Schema)
	if len(mt.RawInputSchema) > 0 {
		var m map[string]any
		if err := json.Unmarshal(mt.RawInputSchema, &m); err == nil {
			return m
		}
	}
	// Convert structured InputSchema
	result := map[string]any{
		"type": "object",
	}
	if mt.InputSchema.Properties != nil {
		result["properties"] = mt.InputSchema.Properties
	} else {
		result["properties"] = map[string]any{}
	}
	if len(mt.InputSchema.Required) > 0 {
		result["required"] = mt.InputSchema.Required
	}
	return result
}

// BridgeTools converts MCP tools from a server into DMR tool.Tool instances.
func BridgeTools(serverName string, conn *Conn) []*tool.Tool {
	rawTools := conn.RawTools()
	tools := make([]*tool.Tool, 0, len(rawTools))
	for _, mt := range rawTools {
		t := BridgeTool(serverName, conn, mt)
		tools = append(tools, t)
	}
	return tools
}

// BridgeTool converts a single MCP tool into a DMR tool.Tool.
func BridgeTool(serverName string, conn *Conn, mt mcpproto.Tool) *tool.Tool {
	dmrName := fmt.Sprintf("mcp_%s_%s", serverName, mt.Name)
	params := inputSchemaToMap(mt)

	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        dmrName,
			Description: mt.Description,
			Parameters:  params,
			Group:       tool.ToolGroupMCP,
		},
		Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
			mcpCtx := context.Background()
			if ctx != nil && ctx.Ctx != nil {
				mcpCtx = ctx.Ctx
			}
			result, err := conn.CallTool(mcpCtx, mt.Name, args)
			if err != nil {
				return nil, fmt.Errorf("mcp call %s/%s: %w", serverName, mt.Name, err)
			}
			return result, nil
		},
	}
}
