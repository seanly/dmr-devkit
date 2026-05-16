package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolvePath resolves a path relative to a base directory.
// Supports the following formats:
//   - Absolute paths: used as-is
//   - ~/ prefix: expanded to user home directory
//   - Relative paths: resolved against baseDir
//
// Examples:
//   - ResolvePath("var/lib/credentials", "~/.dmr") → "~/.dmr/var/lib/credentials"
//   - ResolvePath("~/secrets/key", "~/.dmr") → "~/secrets/key"
//   - ResolvePath("/absolute/path", "~/.dmr") → "/absolute/path"
func ResolvePath(p, baseDir string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}

	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			p = filepath.Join(home, p[2:])
		}
	}

	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}

	if strings.HasPrefix(baseDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			baseDir = filepath.Join(home, baseDir[2:])
		}
	}

	if baseDir != "" {
		return filepath.Clean(filepath.Join(baseDir, p))
	}

	return filepath.Clean(p)
}

// DefaultDataPath returns the default data path for a plugin.
// Pattern: var/lib/<pluginName>/
func DefaultDataPath(baseDir, pluginName string) string {
	return ResolvePath(filepath.Join("var/lib", pluginName), baseDir)
}

// DefaultLogPath returns the default log path for a plugin.
// Pattern: var/log/<pluginName>.log
func DefaultLogPath(baseDir, pluginName string) string {
	return ResolvePath(filepath.Join("var/log", pluginName+".log"), baseDir)
}
