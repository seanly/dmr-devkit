package service

import (
	"testing"

	"github.com/seanly/dmr-devkit/okf/bundle"
)

// TestBacklinksInverseOfNeighbors verifies that for every edge a->b, b's
// backlinks contain a and a's neighbors contain b. This is the core invariant
// the get_backlinks / get_neighbors tools rely on.
func TestBacklinksInverseOfNeighbors(t *testing.T) {
	svc := newServiceFromExample(t)
	g := svc.Graph()

	for _, e := range g.Edges {
		// forward: source references target
		neigh := g.Neighbors(e.Source)
		if !contains(neigh, e.Target) {
			t.Errorf("Neighbors(%q) missing target %q", e.Source, e.Target)
		}
		// reverse: target is backlinked by source
		back := g.Backlinks(e.Target)
		if !contains(back, e.Source) {
			t.Errorf("Backlinks(%q) missing source %q", e.Target, e.Source)
		}
	}
}

// TestGetTrustedDefault verifies the default tier is human-reviewed and that
// filtering by an unknown tier errors.
func TestGetTrustedDefault(t *testing.T) {
	svc := newServiceFromExample(t)

	if _, err := svc.GetTrusted("bogus-tier"); err == nil {
		t.Fatal("expected error for unknown tier")
	}
	// default (empty) and explicit human-reviewed both succeed.
	if _, err := svc.GetTrusted(""); err != nil {
		t.Fatalf("default GetTrusted: %v", err)
	}
	if _, err := svc.GetTrusted("human-reviewed"); err != nil {
		t.Fatalf("explicit GetTrusted: %v", err)
	}
}

// TestGetIndexRoot asserts the root index parses into entries.
func TestGetIndexRoot(t *testing.T) {
	svc := newServiceFromExample(t)
	entries, err := svc.GetIndex("")
	if err != nil {
		t.Fatalf("GetIndex: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected non-empty root index entries")
	}
}

func newServiceFromExample(t *testing.T) *Service {
	t.Helper()
	loader := bundle.NewLoader()
	b, err := loader.Load("testdata/ecommerce-kb")
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	return New(b)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
