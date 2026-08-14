package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CustomType defines a user-extended concept type with its own validation rules.
type CustomType struct {
	Name        string   `yaml:"name"`
	Required    []string `yaml:"required"`
	Description string   `yaml:"description"`
}

// Rules controls validation behavior.
type Rules struct {
	RequireIndex bool `yaml:"require_index"`
	RequireLog   bool `yaml:"require_log"`
	WarnOrphan   bool `yaml:"warn_orphan"`
	WarnUntagged bool `yaml:"warn_untagged"`
}

// Config is the optional .okf.yaml placed at the bundle root.
type Config struct {
	CustomTypes []CustomType `yaml:"custom_types"`
	Rules       Rules        `yaml:"rules"`
}

// DefaultConfig returns an empty config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Rules: Rules{
			RequireIndex: true,
			RequireLog:   false,
			WarnOrphan:   true,
			WarnUntagged: false,
		},
	}
}

// Load reads .okf.yaml from the bundle root if present; otherwise returns defaults.
//
// rules 各字段的默认值仅在对应字段缺失时套用；显式写出（即使 false）则予以保留。
// 这修复了旧实现中 "全 false 被当作缺失" 的误判。
func Load(bundleRoot string) (*Config, error) {
	path := filepath.Join(bundleRoot, ".okf.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse .okf.yaml: %w", err)
	}

	// 探测 rules 中各字段是否显式存在，缺失字段套默认值
	var raw struct {
		Rules map[string]yaml.Node `yaml:"rules"`
	}
	_ = yaml.Unmarshal(data, &raw)
	def := DefaultConfig().Rules
	rulesFields := raw.Rules
	if _, ok := rulesFields["require_index"]; !ok {
		cfg.Rules.RequireIndex = def.RequireIndex
	}
	if _, ok := rulesFields["require_log"]; !ok {
		cfg.Rules.RequireLog = def.RequireLog
	}
	if _, ok := rulesFields["warn_orphan"]; !ok {
		cfg.Rules.WarnOrphan = def.WarnOrphan
	}
	if _, ok := rulesFields["warn_untagged"]; !ok {
		cfg.Rules.WarnUntagged = def.WarnUntagged
	}
	return &cfg, nil
}

// TypeNames returns a set of known custom type names.
func (c *Config) TypeNames() map[string]bool {
	m := make(map[string]bool)
	for _, ct := range c.CustomTypes {
		m[ct.Name] = true
	}
	return m
}

// RequiredFields for a given custom type.
func (c *Config) RequiredFields(typeName string) []string {
	for _, ct := range c.CustomTypes {
		if ct.Name == typeName {
			return ct.Required
		}
	}
	return nil
}
