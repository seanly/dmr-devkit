package a2ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCatalog(t *testing.T) {
	// Create temp files
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(catPath, []byte(`{"catalogId": "test-catalog"}`), 0644); err != nil {
		t.Fatal(err)
	}
	s2cPath := filepath.Join(dir, "s2c.json")
	if err := os.WriteFile(s2cPath, []byte(`{"type": "object"}`), 0644); err != nil {
		t.Fatal(err)
	}
	commonPath := filepath.Join(dir, "common.json")
	if err := os.WriteFile(commonPath, []byte(`{"type": "string"}`), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadCatalog(catPath, s2cPath, commonPath)
	if err != nil {
		t.Fatalf("LoadCatalog error: %v", err)
	}
	if c.CatalogID != "test-catalog" {
		t.Errorf("CatalogID = %q, want %q", c.CatalogID, "test-catalog")
	}
	if c.Name != "basic" {
		t.Errorf("Name = %q, want %q", c.Name, "basic")
	}
	if c.CatalogSchema == nil || c.S2CSchema == nil || c.CommonTypes == nil {
		t.Errorf("expected schemas loaded")
	}
}

func TestLoadCatalogMissing(t *testing.T) {
	_, err := LoadCatalog("/nonexistent/catalog.json", "/nonexistent/s2c.json", "/nonexistent/common.json")
	if err == nil {
		t.Fatal("expected error for missing files")
	}
	if !strings.Contains(err.Error(), "catalog schema") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadCatalogDefaultID(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(catPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	s2cPath := filepath.Join(dir, "s2c.json")
	if err := os.WriteFile(s2cPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	commonPath := filepath.Join(dir, "common.json")
	if err := os.WriteFile(commonPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadCatalog(catPath, s2cPath, commonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.CatalogID, "a2ui.org") {
		t.Errorf("default ID should contain a2ui.org, got %q", c.CatalogID)
	}
}

func TestLoadCatalogWithExamples(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(catPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	s2cPath := filepath.Join(dir, "s2c.json")
	if err := os.WriteFile(s2cPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	commonPath := filepath.Join(dir, "common.json")
	if err := os.WriteFile(commonPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	examplesDir := filepath.Join(dir, "examples")
	if err := os.MkdirAll(examplesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(examplesDir, "ex1.json"), []byte(`{"hello": 1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(examplesDir, "ex2.txt"), []byte(`skip`), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadCatalogWithExamples(catPath, s2cPath, commonPath, examplesDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Examples, "BEGIN ex1") {
		t.Errorf("expected examples to contain ex1")
	}
	if strings.Contains(c.Examples, "ex2") {
		t.Errorf("expected .txt files to be skipped")
	}
}

func TestMustLoadCatalog(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for missing file")
		}
	}()
	MustLoadCatalog("/nonexistent", "/nonexistent", "/nonexistent")
}

func TestLoadJSON(t *testing.T) {
	f, err := os.CreateTemp("", "test.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(`{"key": "value"}`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	m, err := loadJSON(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if m["key"] != "value" {
		t.Errorf("got %v", m)
	}

	_, err = loadJSON("/nonexistent")
	if err == nil {
		t.Errorf("expected error for missing file")
	}

	bad, err := os.CreateTemp("", "bad.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(bad.Name())
	os.WriteFile(bad.Name(), []byte(`{bad`), 0644)
	_, err = loadJSON(bad.Name())
	if err == nil {
		t.Errorf("expected error for invalid JSON")
	}
}

func TestLoadExamplesEmpty(t *testing.T) {
	emptyDir := t.TempDir()
	ex, err := loadExamples(emptyDir)
	if err != nil {
		t.Fatal(err)
	}
	if ex != "" {
		t.Errorf("expected empty, got %q", ex)
	}
}
