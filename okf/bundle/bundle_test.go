package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndSearch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "# Index\n")
	writeFile(t, dir, "tables/orders.md", "---\ntype: table\ntitle: Orders\ndescription: Order table.\ntags: [core]\n---\n\nSchema here.\n")
	writeFile(t, dir, "tables/customers.md", "---\ntype: table\ntitle: Customers\ndescription: Customer table.\ntags: [core]\n---\n\nSchema here.\n")
	writeFile(t, dir, "guidelines/g1.md", "---\ntype: guideline\ntitle: G1\n---\n\nCheck list.\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(b.Concepts) != 3 {
		t.Errorf("concepts = %d, want 3", len(b.Concepts))
	}

	// Search
	results := b.Search("order")
	if len(results) != 1 {
		t.Errorf("search 'order' = %d, want 1", len(results))
	}

	// FilterByType
	tables := b.FilterByType("table")
	if len(tables) != 2 {
		t.Errorf("tables = %d, want 2", len(tables))
	}

	// FilterByTag
	core := b.FilterByTag("core")
	if len(core) != 2 {
		t.Errorf("core tag = %d, want 2", len(core))
	}
}

func TestOrphanedLinks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "---\ntype: concept\n---\n\n[a](b.md)\n")
	writeFile(t, dir, "b.md", "---\ntype: concept\n---\n\nlink to [missing](missing.md)\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	orphans := b.OrphanedLinks()
	if len(orphans["b"]) != 1 {
		t.Errorf("orphans for b = %v, want 1", orphans["b"])
	}
}

func TestAbsoluteLinkResolved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "# Index\n")
	writeFile(t, dir, "tables/orders.md", "---\ntype: table\n---\n\n[customers](/tables/customers.md)\n")
	writeFile(t, dir, "tables/customers.md", "---\ntype: table\n---\nbody\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	links := b.Concepts["tables/orders"].Links
	found := false
	for _, l := range links {
		if l == "tables/customers" {
			found = true
		}
	}
	if !found {
		t.Errorf("absolute link /tables/customers.md not resolved; got %v", links)
	}
}

func TestDirectoryLinkResolved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "# Index\n\n- [Tables](tables/)\n")
	writeFile(t, dir, "tables/index.md", "# Tables\n")
	writeFile(t, dir, "tables/orders.md", "---\ntype: table\n---\nbody\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	// index.md is not a concept; verify the directory link resolves by checking
	// the parsed index entries.
	entries := b.ParseIndex()
	found := false
	for _, e := range entries {
		if e.ID == "tables" {
			found = true
		}
	}
	if !found {
		t.Errorf("directory link tables/ not extracted; got %+v", entries)
	}
}

func TestSubdirIndexLoaded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "# Root\n")
	writeFile(t, dir, "tables/index.md", "# Tables Index\n")
	writeFile(t, dir, "tables/orders.md", "---\ntype: table\n---\nbody\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if b.Indexes["."] == "" || b.Indexes["tables"] == "" {
		t.Errorf("expected root and tables index, got %+v", b.Indexes)
	}
	if b.Index == "" {
		t.Errorf("legacy b.Index should be populated")
	}
}

func TestLogLoaded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "log.md", "# Root Log\n")
	writeFile(t, dir, "tables/log.md", "# Tables Log\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if b.Logs["."] == "" || b.Logs["tables"] == "" {
		t.Errorf("expected root and tables log, got %+v", b.Logs)
	}
}

func TestOKFVersionExtracted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "---\nokf_version: \"0.2\"\n---\n# Index\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if b.OKFVersion != "0.2" {
		t.Errorf("OKFVersion = %q, want 0.2", b.OKFVersion)
	}
}

func TestExcludesGitAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "# Index\n")
	writeFile(t, dir, "a.md", "---\ntype: Metric\n---\nbody\n")
	writeFile(t, dir, ".git/foo.md", "---\ntype: bad\n---\nbody\n")
	writeFile(t, dir, "node_modules/x.md", "---\ntype: bad\n---\nbody\n")
	writeFile(t, dir, ".hidden/y.md", "---\ntype: bad\n---\nbody\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	for id := range b.Concepts {
		if containsStr(id, ".git/") || containsStr(id, "node_modules/") || containsStr(id, ".hidden/") {
			t.Errorf("excluded dir concept loaded: %s", id)
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestVerifiedBareMappingLoadsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "# Index\n")
	writeFile(t, dir, "a.md", "---\ntype: Metric\nverified: { by: human:x, at: 2026-06-25T09:00:00Z }\n---\nbody\n")

	loader := NewLoader()
	b, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	for _, w := range loader.Warnings {
		if containsStr(w, "verified") {
			t.Errorf("bare mapping should not warn: %s", w)
		}
	}
	c := b.Concepts["a"]
	if c.TrustTier() != "human-reviewed" {
		t.Errorf("trust = %q, want human-reviewed", c.TrustTier())
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
