package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarkdownPageStore(t *testing.T) {
	dir := t.TempDir()
	store := newMarkdownPageStore(dir)

	t.Run("WriteAndReadPage", func(t *testing.T) {
		p := Page{
			Slug:      "people/tina-wang",
			Type:      "person",
			Title:     "Tina Wang",
			Content:   "Tina is a senior engineer...",
			Tags:      []string{"engineering", "backend"},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := store.WritePage(p); err != nil {
			t.Fatalf("write page: %v", err)
		}

		// Verify file exists (should be in subdirectory now).
		if _, err := os.Stat(filepath.Join(dir, "people", "tina-wang.md")); err != nil {
			t.Errorf("markdown file missing: %v", err)
		}

		read, err := store.ReadPage("people/tina-wang")
		if err != nil {
			t.Fatalf("read page: %v", err)
		}
		if read == nil {
			t.Fatal("page not found")
		}
		if read.Slug != p.Slug {
			t.Errorf("slug mismatch: %s", read.Slug)
		}
		if read.Title != p.Title {
			t.Errorf("title mismatch: %s", read.Title)
		}
		if read.Content != p.Content {
			t.Errorf("content mismatch: %s", read.Content)
		}
		if len(read.Tags) != 2 || read.Tags[0] != "engineering" {
			t.Errorf("tags mismatch: %v", read.Tags)
		}
	})

	t.Run("DeletePage", func(t *testing.T) {
		p := Page{Slug: "test-delete", Type: "note", Content: "delete me"}
		_ = store.WritePage(p)

		if err := store.DeletePage("test-delete"); err != nil {
			t.Fatalf("delete page: %v", err)
		}

		read, err := store.ReadPage("test-delete")
		if err != nil {
			t.Fatalf("read after delete: %v", err)
		}
		if read != nil {
			t.Error("page should be deleted")
		}
	})

	t.Run("ReadMissingPage", func(t *testing.T) {
		read, err := store.ReadPage("nonexistent")
		if err != nil {
			t.Fatalf("read missing: %v", err)
		}
		if read != nil {
			t.Error("should return nil for missing page")
		}
	})
}

func TestSQLiteBackendWithMarkdown(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		SQLiteDSN:      filepath.Join(dir, "memory.db"),
		ExportDir:      filepath.Join(dir, "pages"),
		EnableFTS5:     false,
		AttachmentRoot: filepath.Join(dir, "attachments"),
	}

	blob, err := NewAttachmentBlobStore(t.Context(), cfg)
	if err != nil {
		t.Fatalf("new blob store: %v", err)
	}
	defer blob.Close()

	backend, err := NewSQLiteBackend(cfg, blob)
	if err != nil {
		t.Fatalf("new backend: %v", err)
	}
	defer backend.Close()

	t.Run("PutPageWritesMarkdown", func(t *testing.T) {
		input := PageInput{
			Type:    "note",
			Title:   "Test Note",
			Content: "This is the content.",
			Tags:    []string{"test", "markdown"},
		}
		_, err := backend.PutPage("test-note", input)
		if err != nil {
			t.Fatalf("put page: %v", err)
		}

		// Verify markdown file was written.
		mdPath := filepath.Join(dir, "pages", "test-note.md")
		if _, err := os.Stat(mdPath); err != nil {
			t.Errorf("markdown file not created: %v", err)
		}
	})

	t.Run("GetPageDoesNotReadMarkdown", func(t *testing.T) {
		// markdown_dir is a read-only backup/export. Direct edits to markdown
		// files must not be read back; DB remains the source of truth.
		mdPath := filepath.Join(dir, "pages", "test-note.md")
		newContent := "---\nslug: test-note\ntype: note\ntitle: Test Note\ntags: [test, markdown]\n---\n\nUpdated content from markdown file.\n"
		if err := os.WriteFile(mdPath, []byte(newContent), 0o644); err != nil {
			t.Fatalf("write markdown: %v", err)
		}

		page, err := backend.GetPage("test-note")
		if err != nil {
			t.Fatalf("get page: %v", err)
		}
		if page == nil {
			t.Fatal("page not found")
		}
		// Content must come from DB, not the edited markdown file.
		if page.Content != "This is the content." {
			t.Errorf("content should come from DB, got: %q", page.Content)
		}
	})

	t.Run("DeletePageRemovesMarkdown", func(t *testing.T) {
		if err := backend.DeletePage("test-note"); err != nil {
			t.Fatalf("delete page: %v", err)
		}

		mdPath := filepath.Join(dir, "pages", "test-note.md")
		if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
			t.Error("markdown file should be deleted")
		}
	})
}
