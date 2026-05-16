package mcp

// ServerConfig describes a single MCP server to connect to.
type ServerConfig struct {
	Name      string
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Transport string // "stdio" or "sse"
}
