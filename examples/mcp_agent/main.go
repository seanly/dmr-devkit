// Example: agent with MCP server tools using pkg/devkit + pkg/mcp.
//
// This example demonstrates how to connect to an external MCP server,
// bridge its tools into a devkit agent, and run a prompt that uses them.
//
// It uses the mcp_server example in this repo as the backend — no npx required.
//
// Step 1: build the MCP server:
//
//	go build -o /tmp/demo-mcp-server ./examples/mcp_server
//
// Step 2a: run agent with stdio (no auth):
//
//	AI_API_KEY=... AI_MODEL=gpt-4o-mini go run ./examples/mcp_agent
//
// Step 2b: start the server in SSE mode with auth, then run agent:
//
//	/tmp/demo-mcp-server -transport sse -addr :9090 -token my-secret-token &
//	AUTH_TOKEN=my-secret-token AI_API_KEY=... AI_MODEL=gpt-4o-mini go run ./examples/mcp_agent -sse
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/mcp"
	"github.com/seanly/dmr-devkit/tool"
)

func main() {
	useSSE := flag.Bool("sse", false, "Connect to SSE server instead of stdio")
	flag.Parse()

	ctx := context.Background()

	// --- 1. Connect to an MCP server ---
	var conn *mcp.Conn
	var err error

	if *useSSE {
		// SSE mode: connect to a remote server with Bearer token auth.
		//
		// The Headers field lets you pass any HTTP headers. For auth,
		// set the Authorization header with a Bearer token that matches
		// what the MCP server expects (see mcp_server example with -token flag).
		token := os.Getenv("AUTH_TOKEN")
		headers := map[string]string{}
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		conn, err = mcp.Connect(ctx, mcp.ServerConfig{
			Name:      "demo",
			URL:       "http://localhost:9090/sse",
			Transport: "sse",
			Headers:   headers,
		})
		if err != nil {
			log.Fatalf("mcp connect (sse): %v\nhint: start the server first: /tmp/demo-mcp-server -transport sse -addr :9090 -token $AUTH_TOKEN", err)
		}
	} else {
		// Stdio mode: spawn the server as a subprocess (no auth needed).
		conn, err = mcp.Connect(ctx, mcp.ServerConfig{
			Name:    "demo",
			Command: "/tmp/demo-mcp-server",
			Args:    []string{"-transport", "stdio"},
		})
		if err != nil {
			log.Fatalf("mcp connect (stdio): %v\nhint: run 'go build -o /tmp/demo-mcp-server ./examples/mcp_server' first", err)
		}
	}
	defer conn.Close()

	// --- 2. Inspect discovered tools ---
	rawTools := conn.RawTools()
	fmt.Printf("Discovered %d MCP tools:\n", len(rawTools))
	for _, t := range rawTools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}
	fmt.Println()

	// --- 3. Build agent with MCP tools + custom tools ---
	mcpTools := mcp.BridgeTools("demo", conn)

	opts := devkit.EnvOptions()
	if opts.APIKey == "" || opts.Model == "" {
		log.Fatal("AI_API_KEY and AI_MODEL are required")
	}
	opts.Verbose = 1
	opts.SystemPromptExtra = "You have access to math and echo tools via MCP. Keep answers concise."

	// Mix MCP tools with a local custom tool
	opts.Tools = append(mcpTools, &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "greet",
			Description: "Generate a friendly greeting for the given name.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Name to greet",
					},
				},
				"required": []any{"name"},
			},
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			name, _ := args["name"].(string)
			return map[string]any{"greeting": fmt.Sprintf("Hello, %s! Nice to meet you.", name)}, nil
		},
	})

	kit, err := devkit.Build(ctx, opts)
	if err != nil {
		log.Fatalf("devkit build: %v", err)
	}
	defer func() { _ = kit.Close(ctx) }()

	// --- 4. Run prompts that exercise MCP + local tools ---
	prompts := []string{
		"Use the add tool to calculate 42 + 17, then echo the result.",
		"Tell me the current server time, then greet the user 'Bob'.",
	}

	for i, p := range prompts {
		fmt.Printf("--- Prompt %d ---\n%s\n\n", i+1, p)
		res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, p, 0)
		if err != nil {
			log.Printf("run %d: %v", i+1, err)
			continue
		}
		fmt.Println(res.Output)
		fmt.Println()
	}
}
