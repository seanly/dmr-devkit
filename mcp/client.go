package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpproto "github.com/mark3labs/mcp-go/mcp"
)

// Conn holds a live MCP client connection and its discovered tools.
type Conn struct {
	Name   string
	client mcpclient.MCPClient
	tools  []mcpproto.Tool
}

// Connect creates an MCP client for the given server config,
// performs the initialize handshake, and discovers tools.
func Connect(ctx context.Context, sc ServerConfig) (*Conn, error) {
	var cli mcpclient.MCPClient
	var err error

	transport := sc.Transport
	if transport == "" {
		if sc.URL != "" {
			transport = "sse"
		} else {
			transport = "stdio"
		}
	}

	switch transport {
	case "stdio":
		if sc.Command == "" {
			return nil, fmt.Errorf("mcp server %q: command is required for stdio transport", sc.Name)
		}
		if err := validateMCPCommand(sc.Command); err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", sc.Name, err)
		}
		env := buildEnv(sc.Env)
		cli, err = mcpclient.NewStdioMCPClient(sc.Command, env, sc.Args...)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q: start stdio: %w", sc.Name, err)
		}
	case "sse":
		if sc.URL == "" {
			return nil, fmt.Errorf("mcp server %q: url is required for sse transport", sc.Name)
		}
		var sseOpts []mcptransport.ClientOption
		if len(sc.Headers) > 0 {
			sseOpts = append(sseOpts, mcptransport.WithHeaders(sc.Headers))
		}
		cli, err = mcpclient.NewSSEMCPClient(sc.URL, sseOpts...)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q: connect sse: %w", sc.Name, err)
		}
	default:
		return nil, fmt.Errorf("mcp server %q: unsupported transport %q", sc.Name, transport)
	}

	// Initialize handshake
	initReq := mcpproto.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpproto.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpproto.Implementation{
		Name:    "dmr-devkit",
		Version: "1.0.0",
	}
	_, err = cli.Initialize(ctx, initReq)
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("mcp server %q: initialize: %w", sc.Name, err)
	}

	// Discover tools
	toolsResult, err := cli.ListTools(ctx, mcpproto.ListToolsRequest{})
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("mcp server %q: list tools: %w", sc.Name, err)
	}

	slog.Info("mcp: server discovered tools", "server", sc.Name, "tools", len(toolsResult.Tools))

	return &Conn{
		Name:   sc.Name,
		client: cli,
		tools:  toolsResult.Tools,
	}, nil
}

// Close closes the MCP client connection.
func (c *Conn) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// RawTools returns the raw MCP tools discovered from the server.
func (c *Conn) RawTools() []mcpproto.Tool {
	if c == nil {
		return nil
	}
	return c.tools
}

// CallTool invokes an MCP tool by name with the given arguments.
func (c *Conn) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	req := mcpproto.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := c.client.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	if result.IsError {
		return "", fmt.Errorf("mcp tool error: %s", extractText(result))
	}
	return extractText(result), nil
}

func buildEnv(envMap map[string]string) []string {
	if len(envMap) == 0 {
		return nil
	}
	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	return env
}

func extractText(result *mcpproto.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcpproto.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func validateMCPCommand(cmd string) error {
	resolved, err := exec.LookPath(cmd)
	if err != nil {
		return fmt.Errorf("command not found in PATH: %w", err)
	}

	realPath, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	allowedPrefixes := []string{
		"/usr/bin/",
		"/usr/local/bin/",
		"/opt/homebrew/bin/",
		"/opt/local/bin/",
		"/bin/",
	}

	if filepath.Separator == '\\' {
		allowedPrefixes = append(allowedPrefixes,
			"C:\\Program Files\\",
			"C:\\Program Files (x86)\\",
			"C:\\Windows\\System32\\",
		)
	}

	for _, p := range allowedPrefixes {
		if strings.HasPrefix(realPath, p) {
			return nil
		}
	}

	return fmt.Errorf("command path %q (resolved to %q) is not in an allowed system directory", cmd, realPath)
}
