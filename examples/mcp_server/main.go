// Example: standalone MCP server with authentication support.
//
// This example uses the mark3labs/mcp-go library directly to build an MCP
// server with three tools (echo, add, time) and demonstrates how to protect
// them with Bearer token authentication on SSE/HTTP transports.
//
// Run as stdio (no auth, default):
//
//	go run ./examples/mcp_server
//
// Run as SSE server with token auth:
//
//	go run ./examples/mcp_server -transport sse -addr :9090 -token my-secret-token
//
// Run as Streamable HTTP with token auth:
//
//	go run ./examples/mcp_server -transport http -addr :9090 -token my-secret-token
//
// Connect from mcp_agent (SSE):
//
//	Set ServerConfig.URL = "http://localhost:9090/sse" and Transport = "sse"
//
// Test with curl:
//
//	curl -H "Authorization: Bearer my-secret-token" http://localhost:9090/sse
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// contextKey is an unexported type for context keys defined in this package.
type contextKey string

const authKey contextKey = "auth_subject"

func main() {
	transport := flag.String("transport", "stdio", "Transport: stdio, sse, or http")
	addr := flag.String("addr", ":9090", "Listen address (sse/http only)")
	token := flag.String("token", "", "Bearer token for auth (sse/http only); empty means no auth")
	flag.Parse()

	// --- Create MCP server ---
	s := server.NewMCPServer(
		"demo-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// --- Register tools ---

	s.AddTool(mcp.NewTool("echo",
		mcp.WithDescription("Echo the input text back."),
		mcp.WithString("message",
			mcp.Description("Text to echo"),
			mcp.Required(),
		),
	), handleEcho)

	s.AddTool(mcp.NewTool("add",
		mcp.WithDescription("Add two numbers and return the result."),
		mcp.WithNumber("a",
			mcp.Description("First number"),
			mcp.Required(),
		),
		mcp.WithNumber("b",
			mcp.Description("Second number"),
			mcp.Required(),
		),
	), handleAdd)

	s.AddTool(mcp.NewTool("current_time",
		mcp.WithDescription("Return the current server time in RFC3339 format."),
	), handleTime)

	// --- Start server ---
	switch *transport {
	case "stdio":
		log.Println("starting MCP server (stdio) — no auth")
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("stdio server: %v", err)
		}

	case "sse":
		var opts []server.SSEOption
		if *token != "" {
			opts = append(opts, server.WithSSEContextFunc(authContextFunc(*token)))
			log.Printf("SSE auth enabled (token: %q)", *token)
		}
		sse := server.NewSSEServer(s, opts...)
		log.Printf("starting MCP server (sse) on %s ...", *addr)
		if err := sse.Start(*addr); err != nil {
			log.Fatalf("sse server: %v", err)
		}

	case "http":
		var opts []server.StreamableHTTPOption
		if *token != "" {
			opts = append(opts, server.WithHTTPContextFunc(authContextFunc(*token)))
			log.Printf("HTTP auth enabled (token: %q)", *token)
		}
		httpServer := server.NewStreamableHTTPServer(s, opts...)
		log.Printf("starting MCP server (streamable-http) on %s ...", *addr)
		if err := httpServer.Start(*addr); err != nil {
			log.Fatalf("http server: %v", err)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown transport %q; use stdio, sse, or http\n", *transport)
		os.Exit(1)
	}
}

// authContextFunc returns a function that validates the Bearer token from the
// Authorization header and injects the authenticated subject into the context.
// If the token is missing or invalid the request is rejected with HTTP 401.
//
// Works as both SSEContextFunc and HTTPContextFunc since they share the same
// signature: func(ctx context.Context, r *http.Request) context.Context.
func authContextFunc(expectedToken string) func(context.Context, *http.Request) context.Context {
	return func(ctx context.Context, r *http.Request) context.Context {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			log.Printf("auth: missing Authorization header from %s", r.RemoteAddr)
			return ctx
		}

		// Expect "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			log.Printf("auth: invalid Authorization format")
			return ctx
		}

		if parts[1] != expectedToken {
			log.Printf("auth: invalid token from %s", r.RemoteAddr)
			return ctx
		}

		// Token is valid — inject subject into context for tool handlers
		return context.WithValue(ctx, authKey, "authenticated-user")
	}
}

// requireAuth is a helper that tool handlers can use to check authentication.
func requireAuth(ctx context.Context) error {
	subject, ok := ctx.Value(authKey).(string)
	if !ok || subject == "" {
		return fmt.Errorf("unauthorized: valid Bearer token required")
	}
	return nil
}

// --- Tool handlers ---

func handleEcho(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireAuth(ctx); err != nil {
		return nil, err
	}
	msg := req.GetString("message", "")
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(msg)},
	}, nil
}

func handleAdd(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireAuth(ctx); err != nil {
		return nil, err
	}
	a := req.GetFloat("a", 0)
	b := req.GetFloat("b", 0)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(fmt.Sprintf("%.6g + %.6g = %.6g", a, b, a+b)),
		},
	}, nil
}

func handleTime(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := requireAuth(ctx); err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(time.Now().Format(time.RFC3339)),
		},
	}, nil
}
