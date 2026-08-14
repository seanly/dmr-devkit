package bundle

import (
	"testing"
)

func TestParseIndex(t *testing.T) {
	b := &Bundle{
		Index: "# Root\n\n## Tables\n\n- [orders](tables/orders.md)\n- [customers](/tables/customers.md)\n",
	}
	entries := b.ParseIndex()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].ID != "tables/orders" {
		t.Errorf("entry[0].ID = %q", entries[0].ID)
	}
	if entries[1].ID != "tables/customers" {
		t.Errorf("entry[1].ID = %q", entries[1].ID)
	}
}

func TestParseIndexAt(t *testing.T) {
	b := &Bundle{
		Indexes: map[string]string{
			".":      "# Root\n",
			"tables": "# Tables\n\n- [orders](orders.md)\n",
		},
	}
	entries := b.ParseIndexAt("tables")
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	// Relative link "orders.md" from tables/index.md → "tables/orders" only if
	// resolveLink-style normalization applied. ParseIndex uses a simpler trim, so
	// the ID is "orders" (relative form). Document this behavior.
	if entries[0].ID != "orders" {
		t.Errorf("entry.ID = %q", entries[0].ID)
	}
}

func TestMissingFromIndex(t *testing.T) {
	b := &Bundle{
		Index: "# Root\n\n- [orders](orders.md)\n",
		Concepts: map[string]*Concept{
			"orders":    {ID: "orders"},
			"customers": {ID: "customers"},
		},
	}
	missing := b.MissingFromIndex()
	if len(missing) != 1 || missing[0] != "customers" {
		t.Errorf("missing = %v, want [customers]", missing)
	}
}
