package skill

import (
	"os"
	"path/filepath"
	"strings"
)

// Config holds skill manager configuration.
type Config struct {
	Paths            []string
	AutoCreatePath   string
	MaxAutoSkills    int
	ArchiveStaleDays int
	AllowCreate      bool
	SecurityScan     bool
	MaxSkillSize     int
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Paths:            []string{filepath.Join(home, ".dmr", "skills", "local")},
		AutoCreatePath:   filepath.Join(home, ".dmr", "skills", "learner"),
		MaxAutoSkills:    20,
		ArchiveStaleDays: 30,
		AllowCreate:      true,
		SecurityScan:     true,
		MaxSkillSize:     65536,
	}
}

// ResolveConfig resolves paths and applies overrides from a map.
func ResolveConfig(base Config, overrides map[string]any) Config {
	if base.MaxSkillSize <= 0 {
		base.MaxSkillSize = 65536
	}
	if base.MaxAutoSkills <= 0 {
		base.MaxAutoSkills = 20
	}
	if base.ArchiveStaleDays <= 0 {
		base.ArchiveStaleDays = 30
	}

	baseDir := "."
	if v, ok := overrides["config_base_dir"].(string); ok && v != "" {
		baseDir = v
	}
	home, _ := os.UserHomeDir()

	if v, ok := overrides["paths"]; ok {
		if arr, ok := v.([]any); ok {
			base.Paths = nil
			for _, item := range arr {
				if s, ok := item.(string); ok {
					expanded := os.ExpandEnv(s)
					if strings.HasPrefix(expanded, "~/") {
						expanded = filepath.Join(home, expanded[2:])
					}
					if !filepath.IsAbs(expanded) {
						expanded = filepath.Join(baseDir, expanded)
					}
					base.Paths = append(base.Paths, expanded)
				}
			}
		}
	}

	if v, ok := overrides["auto_create_path"].(string); ok && v != "" {
		expanded := os.ExpandEnv(v)
		if strings.HasPrefix(expanded, "~/") {
			expanded = filepath.Join(home, expanded[2:])
		}
		if filepath.IsAbs(expanded) {
			base.AutoCreatePath = expanded
		} else {
			base.AutoCreatePath = filepath.Join(baseDir, expanded)
		}
	}

	if v, ok := overrides["max_auto_skills"].(int); ok && v >= 0 {
		base.MaxAutoSkills = v
	} else if v, ok := overrides["max_auto_skills"].(float64); ok && v >= 0 {
		base.MaxAutoSkills = int(v)
	}
	if v, ok := overrides["archive_stale_after_days"].(int); ok && v >= 0 {
		base.ArchiveStaleDays = v
	} else if v, ok := overrides["archive_stale_after_days"].(float64); ok && v >= 0 {
		base.ArchiveStaleDays = int(v)
	}
	if v, ok := overrides["allow_create"].(bool); ok {
		base.AllowCreate = v
	}
	if v, ok := overrides["security_scan"].(bool); ok {
		base.SecurityScan = v
	}
	if v, ok := overrides["max_skill_size"].(int); ok && v > 0 {
		base.MaxSkillSize = v
	} else if v, ok := overrides["max_skill_size"].(float64); ok && v > 0 {
		base.MaxSkillSize = int(v)
	}

	return base
}
