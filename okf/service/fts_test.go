package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
	"github.com/seanly/dmr-devkit/okf/bundle"
	"github.com/seanly/dmr-devkit/okf/search"
)

// writeTempBundleWithIndex is like writeTempBundle but attaches a persistent
// FTS5 index at <root>/.okf/index.db so SearchConcepts uses FTS.
func writeTempBundleWithIndex(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.md":      "---\nokf_version: \"0.2\"\n---\n# KB\n",
		"concepts/a.md": "---\ntype: concept\ntitle: Alpha\ndescription: seed concept\n---\n\nBody of Alpha.\n",
	}
	for rel, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loader := bundle.NewLoader()
	b, err := loader.Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	idx, err := search.Open(filepath.Join(root, ".okf", "index.db"), b.Concepts)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return NewWithIndex(b, bundle.NewWriter(b.RootPath), idx), root
}

func TestSearchConcepts_FTS_CreateUpdateDelete(t *testing.T) {
	svc, _ := writeTempBundleWithIndex(t)

	meta := &frontmatter.Meta{
		Type:        "table",
		Title:       "Orders",
		Description: "customer orders ledger",
		Tags:        []string{"sales"},
	}
	if _, err := svc.CreateConcept("tables/orders", meta, "# Orders\n\ntransaction rows"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Create -> searchable via FTS (3+ codepoint token).
	mustFind := func(query, wantID string) {
		t.Helper()
		results := svc.SearchConcepts(query)
		for _, r := range results {
			if r["id"] == wantID {
				return
			}
		}
		t.Fatalf("SearchConcepts(%q) = %v; want id %q", query, results, wantID)
	}
	mustFind("Orders", "tables/orders")
	mustFind("transaction", "tables/orders")

	// Update body -> new content searchable, old gone.
	if _, err := svc.UpdateConcept("tables/orders", "# Orders\n\nrefund processing"); err != nil {
		t.Fatalf("update: %v", err)
	}
	mustFind("refund", "tables/orders")
	results := svc.SearchConcepts("transaction")
	for _, r := range results {
		if r["id"] == "tables/orders" {
			t.Fatalf("old body token 'transaction' should no longer match after update")
		}
	}

	// Delete -> no longer found.
	if _, err := svc.DeleteConcept("tables/orders"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	results = svc.SearchConcepts("refund")
	for _, r := range results {
		if r["id"] == "tables/orders" {
			t.Fatalf("deleted concept should not appear in search")
		}
	}
}

func TestSearchConcepts_FTSShortQueryFallsBack(t *testing.T) {
	svc, _ := writeTempBundleWithIndex(t)

	// "Alpha" title contains "Al" (2 codepoints) — trigram can't match, so the
	// FTS path returns ErrQueryTooShort and Service falls back to the in-memory
	// Bundle.Search substring scan, which still finds it.
	results := svc.SearchConcepts("Alp") // 3 codepoints -> FTS path
	found := false
	for _, r := range results {
		if r["id"] == "concepts/a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected concepts/a for 'Alp', got %v", results)
	}

	// A 2-codepoint query falls back to in-memory scan and still matches.
	results = svc.SearchConcepts("lp")
	found = false
	for _, r := range results {
		if r["id"] == "concepts/a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("short-query fallback expected concepts/a for 'lp', got %v", results)
	}
}

// TestNewWithoutIndex_NoDbFile ensures the index-free constructors do not
// create a .okf/index.db under the bundle (no pollution of the example bundle
// in newServiceFromExample).
func TestNewWithoutIndex_NoDbFile(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"index.md":      "---\nokf_version: \"0.2\"\n---\n# KB\n",
		"concepts/a.md": "---\ntype: concept\ntitle: A\n---\nbody\n",
	}
	for rel, content := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755)
		_ = os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644)
	}
	loader := bundle.NewLoader()
	b, err := loader.Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	svc := New(b)
	// Search still works (in-memory).
	if results := svc.SearchConcepts("body"); len(results) == 0 {
		t.Fatalf("in-memory search expected results, got 0")
	}
	if _, err := os.Stat(filepath.Join(root, ".okf", "index.db")); !os.IsNotExist(err) {
		t.Fatalf("expected no .okf/index.db with index-free New(), got err=%v", err)
	}
}
