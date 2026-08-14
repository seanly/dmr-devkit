package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, content string) *Config {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".okf.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func TestDefaultsWhenNoConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Rules.RequireIndex || !cfg.Rules.WarnOrphan {
		t.Errorf("expected defaults, got %+v", cfg.Rules)
	}
}

func TestDefaultsWhenNoRulesKey(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `custom_types:
  - name: table
    description: x
`)
	if !cfg.Rules.RequireIndex {
		t.Errorf("RequireIndex should default true when rules absent, got %+v", cfg.Rules)
	}
}

func TestHonorsExplicitAllFalse(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `rules:
  require_index: false
  require_log: false
  warn_orphan: false
  warn_untagged: false
`)
	if cfg.Rules.RequireIndex || cfg.Rules.WarnOrphan {
		t.Errorf("explicit all-false should be honored, got %+v", cfg.Rules)
	}
}

func TestHonorsPartialRules(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `rules:
  warn_orphan: true
`)
	if !cfg.Rules.WarnOrphan {
		t.Errorf("warn_orphan should be true, got %+v", cfg.Rules)
	}
	if !cfg.Rules.RequireIndex {
		t.Errorf("RequireIndex should default true (rules present but field absent), got %+v", cfg.Rules)
	}
}
