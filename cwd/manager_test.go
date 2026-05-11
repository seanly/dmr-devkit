package cwd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()

	m := NewManager(tmpDir, "")

	if m.Get() != normalizePath(tmpDir) {
		t.Errorf("Get() = %q, want %q", m.Get(), tmpDir)
	}

	if m.GetOriginal() != normalizePath(tmpDir) {
		t.Errorf("GetOriginal() = %q, want %q", m.GetOriginal(), tmpDir)
	}

	if m.GetProjectRoot() != normalizePath(tmpDir) {
		t.Errorf("GetProjectRoot() = %q, want %q", m.GetProjectRoot(), tmpDir)
	}
}

func TestSet(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")

	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(tmpDir, "")

	if err := m.Set(subDir); err != nil {
		t.Errorf("Set(%q) error = %v", subDir, err)
	}

	if m.Get() != normalizePath(subDir) {
		t.Errorf("Get() after Set = %q, want %q", m.Get(), subDir)
	}

	// Try to set non-existent directory
	nonExistent := filepath.Join(tmpDir, "does-not-exist")
	if err := m.Set(nonExistent); err == nil {
		t.Error("Set(non-existent) should return error")
	}
}

func TestRecoverIfDeleted(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")

	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(tmpDir, "")
	m.Set(subDir)

	// No recovery needed
	cwd, recovered, err := m.RecoverIfDeleted()
	if err != nil {
		t.Errorf("RecoverIfDeleted() error = %v", err)
	}
	if recovered {
		t.Error("RecoverIfDeleted() = recovered, want false (dir exists)")
	}
	if cwd != normalizePath(subDir) {
		t.Errorf("RecoverIfDeleted() cwd = %q, want %q", cwd, subDir)
	}

	// Delete the subdir and recover
	os.Remove(subDir)

	cwd, recovered, err = m.RecoverIfDeleted()
	if err != nil {
		t.Errorf("RecoverIfDeleted() after delete error = %v", err)
	}
	if !recovered {
		t.Error("RecoverIfDeleted() = not recovered, want true")
	}
	if cwd != normalizePath(tmpDir) {
		t.Errorf("RecoverIfDeleted() recovered to %q, want %q", cwd, tmpDir)
	}
}

func TestMustBeUnder(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")

	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(tmpDir, "")

	// Valid paths under base
	if err := m.MustBeUnder(subDir, tmpDir); err != nil {
		t.Errorf("MustBeUnder(%q, %q) error = %v", subDir, tmpDir, err)
	}

	// Path escaping base
	siblingDir := filepath.Join(filepath.Dir(tmpDir), "sibling")
	if err := m.MustBeUnder(siblingDir, tmpDir); err == nil {
		t.Errorf("MustBeUnder(%q, %q) should return error", siblingDir, tmpDir)
	}

	// Path with .. escaping
	escapePath := filepath.Join(subDir, "..", "..", "escape")
	if err := m.MustBeUnder(escapePath, tmpDir); err == nil {
		t.Errorf("MustBeUnder(%q, %q) should return error for escape path", escapePath, tmpDir)
	}
}

func TestResolve(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, "")

	// Relative path
	rel := "subdir/file.txt"
	resolved := m.Resolve(rel)
	want := normalizePath(filepath.Join(tmpDir, rel))
	if resolved != want {
		t.Errorf("Resolve(%q) = %q, want %q", rel, resolved, want)
	}

	// Absolute path
	abs := "/absolute/path"
	resolved = m.Resolve(abs)
	if resolved != abs {
		t.Errorf("Resolve(%q) = %q, want %q", abs, resolved, abs)
	}
}

func TestGlobalManager(t *testing.T) {
	tmpDir := t.TempDir()

	// Before init
	if GetGlobal() != nil {
		t.Error("GetGlobal() before InitGlobal should return nil")
	}

	if GetGlobalCwd() != "" {
		t.Error("GetGlobalCwd() before InitGlobal should return empty string")
	}

	// After init
	InitGlobal(tmpDir, "")

	if GetGlobal() == nil {
		t.Error("GetGlobal() after InitGlobal should not return nil")
	}

	if GetGlobalCwd() != normalizePath(tmpDir) {
		t.Errorf("GetGlobalCwd() = %q, want %q", GetGlobalCwd(), tmpDir)
	}
}
