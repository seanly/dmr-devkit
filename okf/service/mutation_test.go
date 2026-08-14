package service

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
	"github.com/seanly/dmr-devkit/okf/bundle"
)

// writeTempBundle creates a minimal OKF bundle in a temp dir for mutation
// tests (so the real examples/ecommerce-kb is never touched).
func writeTempBundle(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.md":      "---\nokf_version: \"0.2\"\n---\n# KB\n\n- [A](concepts/a.md)\n",
		"concepts/a.md": "---\ntype: concept\ntitle: A\ndescription: seed concept\n---\n\nBody of A.\n",
	}
	for rel, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	loader := bundle.NewLoader()
	b, err := loader.Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return New(b), root
}

func TestCreateConcept(t *testing.T) {
	svc, root := writeTempBundle(t)
	meta := &frontmatter.Meta{
		Type:        "table",
		Title:       "Orders",
		Description: "customer orders",
		Tags:        []string{"sales"},
	}
	detail, err := svc.CreateConcept("tables/orders", meta, "# Orders\n\nrows")
	if err != nil {
		t.Fatalf("CreateConcept: %v", err)
	}
	if detail["type"] != "table" {
		t.Errorf("detail type = %v", detail["type"])
	}
	// On disk.
	if _, err := os.Stat(filepath.Join(root, "tables", "orders.md")); err != nil {
		t.Errorf("file not written: %v", err)
	}
	// In memory + graph.
	if svc.Bundle().Get("tables/orders") == nil {
		t.Error("concept not in memory")
	}
	g := svc.Graph()
	found := false
	for _, n := range g.Nodes {
		if n.ID == "tables/orders" {
			found = true
		}
	}
	if !found {
		t.Error("new node missing from graph")
	}
}

func TestCreateConceptRejectsMissingType(t *testing.T) {
	svc, _ := writeTempBundle(t)
	_, err := svc.CreateConcept("x", &frontmatter.Meta{Title: "no type"}, "body")
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestUpdateConcept(t *testing.T) {
	svc, _ := writeTempBundle(t)
	_, err := svc.UpdateConcept("concepts/a", "replaced body")
	if err != nil {
		t.Fatalf("UpdateConcept: %v", err)
	}
	c := svc.Bundle().Get("concepts/a")
	if c.Body != "replaced body" {
		t.Errorf("body = %q", c.Body)
	}
	if c.Meta.Title != "A" {
		t.Errorf("frontmatter not preserved: title = %q", c.Meta.Title)
	}
}

func TestSetFrontmatter(t *testing.T) {
	svc, _ := writeTempBundle(t)
	if _, err := svc.SetFrontmatter("concepts/a", "status", "draft"); err != nil {
		t.Fatalf("SetFrontmatter status: %v", err)
	}
	c := svc.Bundle().Get("concepts/a")
	if c.Meta.Status != "draft" {
		t.Errorf("status = %q", c.Meta.Status)
	}
	if _, err := svc.SetFrontmatter("concepts/a", "stale_after", "2026-12-31"); err != nil {
		t.Fatalf("SetFrontmatter stale_after: %v", err)
	}
	c = svc.Bundle().Get("concepts/a")
	if c.Meta.StaleAfter == nil || *c.Meta.StaleAfter != "2026-12-31" {
		t.Errorf("stale_after = %v", c.Meta.StaleAfter)
	}
	if _, err := svc.SetFrontmatter("concepts/a", "bogus", "x"); err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestAppendToConcept(t *testing.T) {
	svc, _ := writeTempBundle(t)
	if _, err := svc.AppendToConcept("concepts/a", "Notes", "extra detail"); err != nil {
		t.Fatalf("AppendToConcept: %v", err)
	}
	c := svc.Bundle().Get("concepts/a")
	if c.Body == "Body of A." || (c.Body != "" && !contains2(c.Body, "extra detail")) {
		t.Errorf("append did not extend body: %q", c.Body)
	}
}

func TestDeleteConcept(t *testing.T) {
	svc, root := writeTempBundle(t)
	if _, err := svc.DeleteConcept("concepts/a"); err != nil {
		t.Fatalf("DeleteConcept: %v", err)
	}
	if svc.Bundle().Get("concepts/a") != nil {
		t.Error("concept still in memory")
	}
	if _, err := os.Stat(filepath.Join(root, "concepts", "a.md")); !os.IsNotExist(err) {
		t.Errorf("file still on disk: %v", err)
	}
	// second delete -> not found
	if _, err := svc.DeleteConcept("concepts/a"); err == nil {
		t.Error("expected not-found on second delete")
	}
}

func TestConcurrencyReadWriteNoRace(t *testing.T) {
	svc, _ := writeTempBundle(t)
	var wg sync.WaitGroup
	// Concurrent readers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = svc.BundleStats()
				_ = svc.ListConcepts("", "")
				_ = svc.Graph()
			}
		}()
	}
	// Concurrent writers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "concepts/c" + itoa(n)
			meta := &frontmatter.Meta{Type: "concept", Title: "C"}
			_, _ = svc.CreateConcept(id, meta, "body")
			_, _ = svc.AppendToConcept(id, "S", "more")
			_, _ = svc.DeleteConcept(id)
		}(i)
	}
	wg.Wait()
}

// contains2 is a local substring check to avoid clashing with the existing
// contains helper for []string.
func contains2(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
