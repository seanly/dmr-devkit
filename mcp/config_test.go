package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	content := `{
  "mcpServers": {
    "local": {
      "command": "bq-mcp",
      "args": ["--project", "p"],
      "env": { "GOOGLE_APPLICATION_CREDENTIALS": "/secrets/bq.json" }
    },
    "remote": {
      "url": "https://wiki.internal/mcp",
      "headers": { "Authorization": "Bearer x" }
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfgs, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	byName := map[string]ServerConfig{}
	for _, c := range cfgs {
		byName[c.Name] = c
	}

	local, ok := byName["local"]
	if !ok {
		t.Fatal("missing local server")
	}
	if local.Transport != "stdio" {
		t.Errorf("local transport = %q, want stdio", local.Transport)
	}
	if local.Command != "bq-mcp" || len(local.Args) != 2 {
		t.Errorf("local command/args unexpected: %+v", local)
	}
	if local.Env["GOOGLE_APPLICATION_CREDENTIALS"] != "/secrets/bq.json" {
		t.Errorf("local env not mapped: %+v", local.Env)
	}

	remote, ok := byName["remote"]
	if !ok {
		t.Fatal("missing remote server")
	}
	if remote.Transport != "sse" {
		t.Errorf("remote transport = %q, want sse", remote.Transport)
	}
	if remote.URL != "https://wiki.internal/mcp" {
		t.Errorf("remote url = %q", remote.URL)
	}
	if remote.Headers["Authorization"] != "Bearer x" {
		t.Errorf("remote headers not mapped: %+v", remote.Headers)
	}
}

func TestLoadConfigFileInvalidEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	content := `{"mcpServers":{"bad":{}}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigFile(path); err == nil {
		t.Fatal("expected error for entry without command or url")
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	if _, err := LoadConfigFile(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfigFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfgs, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if len(cfgs) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfgs))
	}
}
