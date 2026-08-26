package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Integration tests for PostgresBackend. Set MEMORY_POSTGRES_TEST_DSN to a database URL
// (e.g. postgres://user:pass@localhost:5432/dmr_memory_test) to run; otherwise tests skip.
func setupPostgresTest(t *testing.T) *PostgresBackend {
	t.Helper()
	dsn := os.Getenv("MEMORY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("MEMORY_POSTGRES_TEST_DSN not set; skipping postgres integration test")
	}
	dir := t.TempDir()
	cfg := Config{
		PostgresDSN:        dsn,
		EnableFTS5:         true,
		AttachmentRoot:     filepath.Join(dir, "attachments"),
		AttachmentMaxBytes: 100 << 20,
	}
	blob, err := NewAttachmentBlobStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	b, err := NewPostgresBackend(cfg, blob)
	if err != nil {
		_ = blob.Close()
		t.Fatalf("NewPostgresBackend: %v", err)
	}
	t.Cleanup(func() {
		truncatePostgresTestData(t, b)
		_ = b.Close()
		_ = blob.Close()
	})
	truncatePostgresTestData(t, b)
	return b
}

func truncatePostgresTestData(t *testing.T, b *PostgresBackend) {
	t.Helper()
	_, err := b.db.Exec(`TRUNCATE memory_pages RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestPostgresBackend_PutGetDelete(t *testing.T) {
	b := setupPostgresTest(t)

	page, err := b.PutPage("test/pg-put", PageInput{
		Type:    "note",
		Title:   "title",
		Content: "hello world unique content",
		Tags:    []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Slug != "test/pg-put" {
		t.Errorf("slug = %q", page.Slug)
	}
	got, err := b.GetPage("test/pg-put")
	if err != nil || got == nil {
		t.Fatalf("GetPage: %v, got=%v", err, got)
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags = %v", got.Tags)
	}
	if err := b.DeletePage("test/pg-put"); err != nil {
		t.Fatal(err)
	}
	got, _ = b.GetPage("test/pg-put")
	if got != nil {
		t.Error("expected page gone")
	}
}

func TestPostgresBackend_Search(t *testing.T) {
	b := setupPostgresTest(t)
	_, err := b.PutPage("search/doc", PageInput{Content: "golang database plugin"})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := b.SearchPages("golang", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 1 {
		t.Errorf("SearchPages: want >=1 hit, got %d (fulltext may be off)", len(hits))
	}
}

func TestPostgresBackend_LinksAndTimeline(t *testing.T) {
	b := setupPostgresTest(t)
	_, _ = b.PutPage("pg/a", PageInput{Content: "A"})
	_, _ = b.PutPage("pg/b", PageInput{Content: "B"})
	if err := b.CreateLink("pg/a", "pg/b", "mentions", "ctx"); err != nil {
		t.Fatal(err)
	}
	links, err := b.GetLinks("pg/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) < 1 {
		t.Errorf("links: %v", links)
	}
	_ = b.AddTimelineEntry("pg/a", TimelineEntry{EventDate: "2026-01-01", Summary: "e"})
	tl, err := b.GetTimeline("pg/a")
	if err != nil || len(tl) < 1 {
		t.Fatalf("timeline: err=%v n=%d", err, len(tl))
	}
}
