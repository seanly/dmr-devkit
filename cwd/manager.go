// Package cwd provides working directory management for DMR.
// It tracks the current working directory, handles recovery when CWD is deleted,
// and prevents directory escape attempts.
package cwd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Manager tracks the current working directory and provides recovery mechanisms.
type Manager struct {
	mu          sync.RWMutex
	originalCwd string // The CWD when DMR started (anchor point)
	currentCwd  string // The current working directory
	projectRoot string // Project root directory (may differ from originalCwd in worktree scenarios)
}

// NewManager creates a new CWD manager with the given original and project root directories.
// If projectRoot is empty, it defaults to originalCwd.
func NewManager(originalCwd, projectRoot string) *Manager {
	if projectRoot == "" {
		projectRoot = originalCwd
	}
	return &Manager{
		originalCwd: normalizePath(originalCwd),
		currentCwd:  normalizePath(originalCwd),
		projectRoot: normalizePath(projectRoot),
	}
}

// Get returns the current working directory.
func (m *Manager) Get() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentCwd
}

// GetOriginal returns the original working directory (startup CWD).
func (m *Manager) GetOriginal() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.originalCwd
}

// GetProjectRoot returns the project root directory.
// Use this for project identity (history, skills) not file operations.
func (m *Manager) GetProjectRoot() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projectRoot
}

// Set updates the current working directory.
// Returns error if the directory does not exist or is not accessible.
func (m *Manager) Set(cwd string) error {
	cwd = normalizePath(cwd)

	// Verify the directory exists
	info, err := os.Stat(cwd)
	if err != nil {
		return fmt.Errorf("cannot set cwd to %q: %w", cwd, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cannot set cwd to %q: not a directory", cwd)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentCwd = cwd
	return nil
}

// RecoverIfDeleted checks if the current CWD still exists.
// If not, it attempts to recover to a valid directory in priority order:
// 1. projectRoot
// 2. originalCwd
// 3. home directory
// 4. root directory
// Returns the recovered CWD and whether a recovery was needed.
func (m *Manager) RecoverIfDeleted() (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if current CWD still exists
	if _, err := os.Stat(m.currentCwd); err == nil {
		return m.currentCwd, false, nil // No recovery needed
	}

	// Try recovery in priority order
	candidates := []string{
		m.projectRoot,
		m.originalCwd,
	}

	// Add home directory
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, home)
	}

	// Add root as last resort
	candidates = append(candidates, "/")

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			m.currentCwd = normalizePath(candidate)
			return m.currentCwd, true, nil
		}
	}

	return "", false, fmt.Errorf("could not recover CWD: all fallback directories unavailable")
}

// MustBeUnder checks if a path is under a base directory.
// Returns error if the path escapes the base directory.
func (m *Manager) MustBeUnder(path, base string) error {
	path = normalizePath(path)
	base = normalizePath(base)

	// Get absolute paths
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("cannot resolve path %q: %w", path, err)
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return fmt.Errorf("cannot resolve base %q: %w", base, err)
	}

	// Ensure base ends with separator for prefix check
	if !strings.HasSuffix(absBase, string(filepath.Separator)) {
		absBase += string(filepath.Separator)
	}

	if !strings.HasPrefix(absPath+string(filepath.Separator), absBase) {
		return fmt.Errorf("path %q escapes base directory %q", path, base)
	}
	return nil
}

// Resolve resolves a path relative to the current CWD.
// If the path is absolute, it is returned as-is.
// If the path is relative, it is resolved against the current CWD.
func (m *Manager) Resolve(path string) string {
	if filepath.IsAbs(path) {
		return normalizePath(path)
	}
	return normalizePath(filepath.Join(m.Get(), path))
}

// normalizePath normalizes a path for consistent comparison.
// It cleans the path and ensures consistent separator usage.
func normalizePath(path string) string {
	// Clean the path (remove .., ., duplicate separators)
	path = filepath.Clean(path)

	// Ensure consistent separator
	path = filepath.ToSlash(path)

	return path
}

// Global manager instance (for simple use cases)
var globalManager *Manager
var globalMu sync.RWMutex

// InitGlobal initializes the global CWD manager.
// Should be called once at startup.
func InitGlobal(originalCwd, projectRoot string) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalManager = NewManager(originalCwd, projectRoot)
}

// GetGlobal returns the global CWD manager.
// Returns nil if not initialized.
func GetGlobal() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalManager
}

// GetGlobalCwd returns the current CWD from the global manager.
// Returns empty string if not initialized.
func GetGlobalCwd() string {
	m := GetGlobal()
	if m == nil {
		return ""
	}
	return m.Get()
}
