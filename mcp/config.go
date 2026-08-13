package mcp

import (
	"encoding/json"
	"fmt"
	"os"
)

// ServerConfig describes a single MCP server to connect to.
type ServerConfig struct {
	Name      string
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Transport string            // "stdio" or "sse"
	Headers   map[string]string // HTTP headers sent with every request (sse transport)
}

// ConfigFile is the standard JSON shape for MCP server configuration, mirroring
// the `mcpServers` convention used by Claude Code / Cursor. It is a
// tooling-level config (runtime connections, often carrying secrets) intended to
// live separate from application content config.
type ConfigFile struct {
	McpServers map[string]ServerEntry `json:"mcpServers"`
}

// ServerEntry is one server definition. stdio servers use command/args/env;
// remote servers use url/headers. Transport is inferred from which is present.
type ServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// LoadConfigFile parses a standard mcp.json file into []ServerConfig.
// stdio servers (command present) get Transport="stdio"; remote servers
// (url present) get Transport="sse". An entry with neither is an error.
func LoadConfigFile(path string) ([]ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mcp config %s: %w", path, err)
	}
	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp config %s: %w", path, err)
	}

	var out []ServerConfig
	for name, e := range cfg.McpServers {
		sc := ServerConfig{
			Name:    name,
			Command: e.Command,
			Args:    e.Args,
			Env:     e.Env,
			URL:     e.URL,
			Headers: e.Headers,
		}
		switch {
		case e.URL != "":
			sc.Transport = "sse"
		case e.Command != "":
			sc.Transport = "stdio"
		default:
			return nil, fmt.Errorf("mcp server %q: must define either \"command\" (stdio) or \"url\" (sse)", name)
		}
		out = append(out, sc)
	}
	return out, nil
}
