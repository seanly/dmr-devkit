package search

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
	"github.com/seanly/dmr-devkit/okf/bundle"
)

// concept is a tiny helper to build a *bundle.Concept with an absolute file
// path under root, so the index's mtime stat works during reconciliation.
func concept(root, id, title, typ, desc, body string, tags ...string) *bundle.Concept {
	return &bundle.Concept{
		ID:       id,
		Meta:     &frontmatter.Meta{Type: typ, Title: title, Description: desc, Tags: tags},
		Body:     body,
		FilePath: filepath.Join(root, filepath.FromSlash(id+".md")),
	}
}

// writeConceptFile materializes the .md file on disk so stat/mtime works.
func writeConceptFile(t *testing.T, c *bundle.Concept) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(c.FilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.FilePath, []byte(c.Body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSearch_RankingAndCJK(t *testing.T) {
	root := t.TempDir()
	a := concept(root, "tables/orders", "订单表", "table", "core orders", "订单 订单详情 order transactions")
	b := concept(root, "tables/customers", "客户表", "table", "customer records", "客户信息 customer profiles")
	writeConceptFile(t, a)
	writeConceptFile(t, b)

	dbPath := filepath.Join(root, ".okf", "index.db")
	idx, err := Open(dbPath, map[string]*bundle.Concept{a.ID: a, b.ID: b})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer idx.Close()

	// Chinese substring of 3+ codepoints: "订单" is 2 codepoints -> fallback.
	if _, err := idx.Search("订单", 10); err != ErrQueryTooShort {
		t.Fatalf("expected ErrQueryTooShort for 2-codepoint token, got %v", err)
	}

	// "订单详情" (4 codepoints) matches orders via body.
	hits, err := idx.Search("订单详情", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "tables/orders" {
		t.Fatalf("expected orders only, got %+v", hits)
	}

	// English token in body matches both; title match should rank first.
	hits, err = idx.Search("customer", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "tables/customers" {
		t.Fatalf("expected customers, got %+v", hits)
	}
}

func TestSearch_PersistenceAcrossReopen(t *testing.T) {
	root := t.TempDir()
	c := concept(root, "tables/orders", "Orders", "table", "core orders", "order transactions")
	writeConceptFile(t, c)

	dbPath := filepath.Join(root, ".okf", "index.db")
	idx, err := Open(dbPath, map[string]*bundle.Concept{c.ID: c})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	idx.Close()

	// Reopen with an empty concept map: reconciliation should DROP the row
	// (concept no longer present), so search returns nothing.
	idx2, err := Open(dbPath, map[string]*bundle.Concept{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()
	hits, err := idx2.Search("order", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits after reconcile-drop, got %d", len(hits))
	}

	// Reopen again WITH the concept: row should be re-indexed (no crash).
	idx3, err := Open(dbPath, map[string]*bundle.Concept{c.ID: c})
	if err != nil {
		t.Fatalf("reopen with concept: %v", err)
	}
	defer idx3.Close()
	hits, err = idx3.Search("order", 10)
	if err != nil {
		t.Fatalf("search after reindex: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "tables/orders" {
		t.Fatalf("expected orders reindexed, got %+v", hits)
	}
}

func TestReconcile_DetectsContentChangeAndNoChange(t *testing.T) {
	root := t.TempDir()
	c := concept(root, "tables/orders", "Orders", "table", "core orders", "old body content")
	writeConceptFile(t, c)

	dbPath := filepath.Join(root, ".okf", "index.db")
	idx, err := Open(dbPath, map[string]*bundle.Concept{c.ID: c})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	idx.Close()

	// Change the file content + mtime; reopen and reconcile must re-index.
	time.Sleep(10 * time.Millisecond) // ensure mtime advances
	c2 := concept(root, "tables/orders", "Orders", "table", "core orders", "brand new content")
	writeConceptFile(t, c2)
	idx2, err := Open(dbPath, map[string]*bundle.Concept{c2.ID: c2})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()
	hits, err := idx2.Search("brand", 10)
	if err != nil {
		t.Fatalf("search new content: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected reindexed new content to match, got %d", len(hits))
	}

	// Old content should no longer match.
	hits, err = idx2.Search("old body", 10)
	if err != nil {
		t.Fatalf("search old content: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected old content gone, got %d", len(hits))
	}
}

func TestUpsertRemove(t *testing.T) {
	root := t.TempDir()
	c := concept(root, "tables/orders", "Orders", "table", "", "order transactions")
	writeConceptFile(t, c)

	idx, err := Open(filepath.Join(root, ".okf", "index.db"), map[string]*bundle.Concept{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer idx.Close()

	// Upsert then search.
	if err := idx.Upsert(c); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	hits, err := idx.Search("order", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit after upsert, got %d", len(hits))
	}

	// Remove then search.
	if err := idx.Remove(c.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	hits, err = idx.Search("order", 10)
	if err != nil {
		t.Fatalf("search after remove: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits after remove, got %d", len(hits))
	}

	// Remove again is idempotent (no error).
	if err := idx.Remove(c.ID); err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
}

func TestSearch_SpecialCharsNoInjection(t *testing.T) {
	root := t.TempDir()
	c := concept(root, "a/b", "Title", "table", "", `has "quotes" and AND keyword`)
	writeConceptFile(t, c)
	idx, err := Open(filepath.Join(root, ".okf", "index.db"), map[string]*bundle.Concept{c.ID: c})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer idx.Close()

	// A query with FTS5-special tokens must not error or match unexpectedly.
	// "quotes" is a real token in the body -> match.
	hits, err := idx.Search(`quotes`, 10)
	if err != nil {
		t.Fatalf("search quotes: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for quotes, got %d", len(hits))
	}

	// A literal "AND" token (3 codepoints) is treated as a string literal,
	// not the FTS5 AND operator — it matches the body text "AND".
	hits, err = idx.Search("AND", 10)
	if err != nil {
		t.Fatalf("search AND: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected AND matched as literal, got %d", len(hits))
	}
}
