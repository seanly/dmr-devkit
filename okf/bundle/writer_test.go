package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
)

func TestWriterWriteConcept(t *testing.T) {
	root := t.TempDir()
	w := NewWriter(root)
	meta := &frontmatter.Meta{
		Type:        "table",
		Title:       "Orders",
		Description: "Customer orders",
		Tags:        []string{"sales"},
	}
	if err := w.WriteConcept("tables/orders", meta, "# Orders\n\nrows"); err != nil {
		t.Fatalf("WriteConcept: %v", err)
	}
	// File exists at nested path.
	data, err := os.ReadFile(filepath.Join(root, "tables", "orders.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("missing frontmatter:\n%s", s)
	}
	if !strings.Contains(s, "type: table") {
		t.Errorf("type not written:\n%s", s)
	}
	if !strings.Contains(s, "# Orders") {
		t.Errorf("body not written:\n%s", s)
	}

	// Round-trip via Loader.
	loader := NewLoader()
	b, err := loader.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := b.Get("tables/orders")
	if c == nil {
		t.Fatal("concept not loaded")
	}
	if c.Meta.Type != "table" || c.Meta.Title != "Orders" {
		t.Errorf("loaded meta mismatch: %+v", c.Meta)
	}
}

func TestWriterOverwriteExisting(t *testing.T) {
	root := t.TempDir()
	w := NewWriter(root)
	meta := &frontmatter.Meta{Type: "concept", Title: "v1"}
	if err := w.WriteConcept("note", meta, "body1"); err != nil {
		t.Fatal(err)
	}
	meta.Title = "v2"
	if err := w.WriteConcept("note", meta, "body2"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "note.md"))
	s := string(data)
	if !strings.Contains(s, "title: v2") || !strings.Contains(s, "body2") {
		t.Errorf("overwrite failed:\n%s", s)
	}
	if strings.Contains(s, "v1") || strings.Contains(s, "body1") {
		t.Errorf("stale content remained:\n%s", s)
	}
}

func TestWriterDelete(t *testing.T) {
	root := t.TempDir()
	w := NewWriter(root)
	meta := &frontmatter.Meta{Type: "concept"}
	if err := w.WriteConcept("note", meta, "x"); err != nil {
		t.Fatal(err)
	}
	if err := w.Delete("note"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "note.md")); !os.IsNotExist(err) {
		t.Errorf("file should be gone: %v", err)
	}
	// idempotent
	if err := w.Delete("note"); err != nil {
		t.Errorf("idempotent delete: %v", err)
	}
}

func TestWriterAppendLog(t *testing.T) {
	root := t.TempDir()
	w := NewWriter(root)
	if err := w.AppendLog(".", "- 2026-08-13 created orders\n"); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	if err := w.AppendLog(".", "- 2026-08-14 updated pricing\n"); err != nil {
		t.Fatalf("AppendLog 2: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "log.md"))
	s := string(data)
	if !strings.Contains(s, "created orders") || !strings.Contains(s, "updated pricing") {
		t.Errorf("log entries missing:\n%s", s)
	}
}

func TestWriterRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	w := NewWriter(root)
	meta := &frontmatter.Meta{Type: "concept"}
	cases := []string{"../escape", "a/../../b", "../"}
	for _, id := range cases {
		if err := w.WriteConcept(id, meta, "x"); err == nil {
			t.Errorf("expected error for id %q", id)
		}
	}
}
