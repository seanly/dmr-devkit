package memory

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func setupBackend(t *testing.T) *SQLiteBackend {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		SQLiteDSN:          filepath.Join(dir, "test.db"),
		EnableFTS5:         true,
		AttachmentRoot:     filepath.Join(dir, "attachments"),
		AttachmentMaxBytes: 100 << 20,
	}
	blob, err := NewAttachmentBlobStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	b, err := NewSQLiteBackend(cfg, blob)
	if err != nil {
		_ = blob.Close()
		t.Fatalf("create backend: %v", err)
	}
	t.Cleanup(func() {
		_ = b.Close()
		_ = blob.Close()
	})
	return b
}

// --- Pages ---

func TestPutPage_CreateAndGet(t *testing.T) {
	b := setupBackend(t)

	page, err := b.PutPage("people/tina-wang", PageInput{
		Type:    "person",
		Title:   "Tina Wang",
		Content: "Sequoia China partner, focuses on AI",
		Tags:    []string{"investor", "ai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Slug != "people/tina-wang" {
		t.Errorf("slug = %q, want people/tina-wang", page.Slug)
	}
	if page.Type != "person" {
		t.Errorf("type = %q, want person", page.Type)
	}
	if len(page.Tags) != 2 {
		t.Errorf("tags = %v, want 2", page.Tags)
	}

	got, err := b.GetPage("people/tina-wang")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("page not found")
	}
	if got.Title != "Tina Wang" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestPutPage_Upsert(t *testing.T) {
	b := setupBackend(t)

	_, err := b.PutPage("notes/test", PageInput{Content: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := b.PutPage("notes/test", PageInput{Content: "v2", Title: "Updated"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Content != "v2" {
		t.Errorf("content = %q, want v2", page.Content)
	}
	if page.Title != "Updated" {
		t.Errorf("title = %q", page.Title)
	}
}

func TestRevisions_ListAndRevert(t *testing.T) {
	b := setupBackend(t)
	_, err := b.PutPage("notes/rev", PageInput{Content: "one"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.PutPage("notes/rev", PageInput{Content: "two"})
	if err != nil {
		t.Fatal(err)
	}
	revs, err := b.ListRevisions("notes/rev")
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("want 1 revision, got %d", len(revs))
	}
	p, _ := b.GetPage("notes/rev")
	if p == nil || p.Content != "two" {
		t.Fatalf("before revert, want content two, got %v", p)
	}
	if err := b.RevertToRevision("notes/rev", revs[0].ID); err != nil {
		t.Fatal(err)
	}
	p, _ = b.GetPage("notes/rev")
	if p == nil || p.Content != "one" {
		t.Errorf("after revert, want one, got %v", p)
	}
	st, err := b.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Backend != "sqlite" || st.PageCount < 1 || st.RevisionCount < 1 {
		t.Errorf("Stats() = %+v", st)
	}
}

func TestGetPage_NotFound(t *testing.T) {
	b := setupBackend(t)
	got, err := b.GetPage("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent page")
	}
}

func TestDeletePage(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("notes/del", PageInput{Content: "to delete"})
	if err := b.DeletePage("notes/del"); err != nil {
		t.Fatal(err)
	}
	got, _ := b.GetPage("notes/del")
	if got != nil {
		t.Error("page should be deleted")
	}
}

func TestDeletePage_NotFound(t *testing.T) {
	b := setupBackend(t)
	err := b.DeletePage("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent page")
	}
}

func TestDeletePage_Cascades(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("people/a", PageInput{Content: "A", Tags: []string{"tag1"}})
	_, _ = b.PutPage("people/b", PageInput{Content: "B"})
	_ = b.CreateLink("people/a", "people/b", "mentions", "")
	_ = b.AddTimelineEntry("people/a", TimelineEntry{EventDate: "2026-01-01", Summary: "event"})

	_ = b.DeletePage("people/a")

	// Tags, links, timeline should be gone
	tags, _ := b.GetTags("people/a")
	if len(tags) != 0 {
		t.Errorf("tags should be empty after delete, got %v", tags)
	}
	links, _ := b.GetLinks("people/b")
	for _, l := range links {
		if l.Slug == "people/a" {
			t.Error("link from deleted page should be gone")
		}
	}
}

// --- Search ---

func TestSearchPages_FTS5(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("people/tina", PageInput{Title: "Tina Wang", Content: "Sequoia partner focusing on AI startups"})
	_, _ = b.PutPage("people/bob", PageInput{Title: "Bob Lee", Content: "CTO at a fintech company"})
	_, _ = b.PutPage("companies/seq", PageInput{Title: "Sequoia Capital", Content: "VC firm"})

	results, err := b.SearchPages("Sequoia", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(results))
	}
}

func TestSearchPages_NoResults(t *testing.T) {
	b := setupBackend(t)
	results, err := b.SearchPages("nothing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// --- Tags ---

func TestTags_AddRemove(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("notes/test", PageInput{Content: "test"})

	if err := b.AddTags("notes/test", []string{"a", "B", "C"}); err != nil {
		t.Fatal(err)
	}
	tags, _ := b.GetTags("notes/test")
	if len(tags) != 3 {
		t.Errorf("tags = %v, want 3", tags)
	}

	if err := b.RemoveTags("notes/test", []string{"b"}); err != nil {
		t.Fatal(err)
	}
	tags, _ = b.GetTags("notes/test")
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2 after remove", tags)
	}
}

func TestTags_Dedup(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("notes/dedup", PageInput{Content: "test", Tags: []string{"x"}})
	_ = b.AddTags("notes/dedup", []string{"x", "y"})

	tags, _ := b.GetTags("notes/dedup")
	count := 0
	for _, t := range tags {
		if t == "x" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 occurrence of tag 'x', got %d", count)
	}
}

// --- Links ---

func TestLinks_CreateAndGet(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("people/alice", PageInput{Content: "Alice"})
	_, _ = b.PutPage("companies/acme", PageInput{Content: "Acme"})

	err := b.CreateLink("people/alice", "companies/acme", "works_at", "CEO")
	if err != nil {
		t.Fatal(err)
	}

	links, err := b.GetLinks("people/alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("links = %v, want 1", links)
	}
	if links[0].Slug != "companies/acme" {
		t.Errorf("link slug = %q", links[0].Slug)
	}
	if links[0].Direction != "outgoing" {
		t.Errorf("direction = %q", links[0].Direction)
	}

	// Backlink
	backlinks, _ := b.GetLinks("companies/acme")
	if len(backlinks) != 1 {
		t.Fatalf("backlinks = %v, want 1", backlinks)
	}
	if backlinks[0].Slug != "people/alice" {
		t.Errorf("backlink slug = %q", backlinks[0].Slug)
	}
	if backlinks[0].Direction != "incoming" {
		t.Errorf("direction = %q", backlinks[0].Direction)
	}
}

func TestLinks_DeleteByType(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("a", PageInput{Content: "A"})
	_, _ = b.PutPage("b", PageInput{Content: "B"})

	_ = b.CreateLink("a", "b", "mentions", "")
	_ = b.CreateLink("a", "b", "works_at", "")

	_ = b.DeleteLink("a", "b", "mentions")
	links, _ := b.GetLinks("a")
	if len(links) != 1 || links[0].LinkType != "works_at" {
		t.Errorf("after delete, links = %v", links)
	}
}

func TestLinks_MissingPage(t *testing.T) {
	b := setupBackend(t)
	err := b.CreateLink("nonexistent", "also-nonexistent", "mentions", "")
	if err == nil {
		t.Error("expected error for missing page")
	}
}

// --- Timeline ---

func TestTimeline_AddAndGet(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("people/alice", PageInput{Content: "Alice"})

	err := b.AddTimelineEntry("people/alice", TimelineEntry{
		EventDate: "2026-04-18",
		Summary:   "Joined Acme as CEO",
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := b.GetTimeline("people/alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want 1", entries)
	}
	if entries[0].EventDate != "2026-04-18" {
		t.Errorf("date = %q", entries[0].EventDate)
	}
}

func TestTimeline_Dedup(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("notes/test", PageInput{Content: "test"})

	_ = b.AddTimelineEntry("notes/test", TimelineEntry{EventDate: "2026-01-01", Summary: "same"})
	_ = b.AddTimelineEntry("notes/test", TimelineEntry{EventDate: "2026-01-01", Summary: "same"})

	entries, _ := b.GetTimeline("notes/test")
	if len(entries) != 1 {
		t.Errorf("expected 1 deduplicated entry, got %d", len(entries))
	}
}

// --- List ---

func TestListPages_FilterByTag(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("a", PageInput{Content: "A", Tags: []string{"alpha"}})
	_, _ = b.PutPage("b", PageInput{Content: "B", Tags: []string{"beta"}})
	_, _ = b.PutPage("c", PageInput{Content: "C", Tags: []string{"alpha", "beta"}})

	pages, err := b.ListPages(ListOpts{Tag: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Errorf("expected 2 pages with tag alpha, got %d", len(pages))
	}
}

func TestListPages_FilterBySlugPrefix(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("people/alice", PageInput{Content: "A"})
	_, _ = b.PutPage("people/bob", PageInput{Content: "B"})
	_, _ = b.PutPage("companies/acme", PageInput{Content: "C"})

	pages, err := b.ListPages(ListOpts{SlugPrefix: "people/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Errorf("expected 2 people pages, got %d", len(pages))
	}
}

// --- Security ---

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		slug string
		ok   bool
	}{
		{"people/tina-wang", true},
		{"config/user-prefs", true},
		{"a", true},
		{"", false},
		{"UPPER", false},
		{"has space", false},
		{"/leading-slash", false},
		{"trailing/", false},
		{"double--hyphen", true},
		{"people//double", false},
	}
	for _, tt := range tests {
		err := ValidateSlug(tt.slug)
		if (err == nil) != tt.ok {
			t.Errorf("ValidateSlug(%q) = %v, want ok=%v", tt.slug, err, tt.ok)
		}
	}
}

// --- Config ---

func TestParseConfig_Defaults(t *testing.T) {
	cfg := ParseConfig(map[string]any{}, t.TempDir())
	if cfg.SQLiteDSN == "" {
		t.Error("DSN should have a default")
	}
	if !cfg.EnableFTS5 {
		t.Error("FTS5 should default to true")
	}
}

func TestParseConfig_CustomDSN(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "custom.db")
	cfg := ParseConfig(map[string]any{
		"sqlite_dsn": dsn,
	}, dir)
	if cfg.SQLiteDSN != dsn {
		t.Errorf("DSN = %q, want %q", cfg.SQLiteDSN, dsn)
	}
}

// --- Pinned ---

func TestGetPinnedPages(t *testing.T) {
	b := setupBackend(t)
	_, _ = b.PutPage("config/user-prefs", PageInput{Type: "config", Content: "prefers Chinese"})
	_, _ = b.PutPage("notes/unpinned", PageInput{Content: "not pinned"})

	pinned, err := b.GetPinnedPages()
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 1 {
		t.Errorf("expected 1 pinned page, got %d", len(pinned))
	}
	if pinned[0].Slug != "config/user-prefs" {
		t.Errorf("pinned slug = %q", pinned[0].Slug)
	}
}

// --- NewBackend from plugin.go init ---

func TestPluginInit(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		SQLiteDSN:          filepath.Join(dir, "plugin-test.db"),
		EnableFTS5:         true,
		AttachmentRoot:     filepath.Join(dir, "attachments"),
		AttachmentMaxBytes: 100 << 20,
	}
	blob, err := NewAttachmentBlobStore(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSQLiteBackend(cfg, blob)
	if err != nil {
		_ = blob.Close()
		t.Fatal(err)
	}
	defer b.Close()
	defer blob.Close()

	if !b.HasFTS5() {
		t.Error("FTS5 should be enabled")
	}

	// Verify WAL mode
	var mode string
	_ = b.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

// --- HasFTS5 ---

func TestHasFTS5(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		SQLiteDSN:          filepath.Join(dir, "nofts5.db"),
		EnableFTS5:         false,
		AttachmentRoot:     filepath.Join(dir, "attachments"),
		AttachmentMaxBytes: 100 << 20,
	}
	blob, err := NewAttachmentBlobStore(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSQLiteBackend(cfg, blob)
	if err != nil {
		_ = blob.Close()
		t.Fatal(err)
	}
	defer b.Close()
	defer blob.Close()

	if b.HasFTS5() {
		t.Error("FTS5 should be disabled")
	}
	_ = os.Remove(filepath.Join(dir, "nofts5.db"))
}

func TestMemoryAttachments_SQLite(t *testing.T) {
	b := setupBackend(t)
	if _, err := b.PutPage("notes/att-doc", PageInput{Title: "T", Content: "c"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	payload := []byte("hello-attachment-bytes")
	a, err := b.PutAttachment(ctx, "notes/att-doc", AttachmentInput{
		Name: "note.bin", Mime: "application/octet-stream", Summary: "unique summary token xyz",
	}, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.PublicID == "" {
		t.Fatal("expected attachment")
	}
	list, err := b.ListAttachments("notes/att-doc", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListAttachments: err=%v n=%d", err, len(list))
	}
	rc, meta, err := b.OpenAttachment(ctx, a.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("body mismatch")
	}
	if meta.Size != int64(len(payload)) {
		t.Fatalf("size = %d", meta.Size)
	}
	hits, err := b.SearchAttachments("xyz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 1 {
		t.Fatalf("SearchAttachments: want >=1 got %d (fts=%v)", len(hits), b.hasAttachmentFTS5)
	}
	if err := b.DeleteAttachment(ctx, a.PublicID); err != nil {
		t.Fatal(err)
	}
	list, _ = b.ListAttachments("notes/att-doc", 10)
	if len(list) != 0 {
		t.Fatalf("after delete want 0 attachments, got %d", len(list))
	}
}

func TestMemoryAttachments_DeletePageCascade(t *testing.T) {
	b := setupBackend(t)
	if _, err := b.PutPage("notes/gone", PageInput{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, err := b.PutAttachment(ctx, "notes/gone", AttachmentInput{Name: "a.dat"}, bytes.NewReader([]byte("z")), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.DeletePage("notes/gone"); err != nil {
		t.Fatal(err)
	}
	if m, _ := b.GetAttachment(a.PublicID); m != nil {
		t.Fatal("attachment should be removed with page")
	}
}
