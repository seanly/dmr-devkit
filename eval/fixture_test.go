package eval

import (
	"path/filepath"
	"testing"
)

func TestRunBaselineFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "dmr-testdata", "eval", "fixtures")
	passed, total, _, err := RunBaselineFixtures(dir)
	if err != nil {
		t.Skipf("fixtures not available: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 fixtures, got %d", total)
	}
	if passed != total {
		t.Fatalf("expected all fixtures to pass, got %d/%d", passed, total)
	}
}

func TestLoadTapeEntries(t *testing.T) {
	path := filepath.Join("..", "..", "dmr-testdata", "eval", "tapes", "handoff_recovery.json")
	entries, err := LoadTapeEntries(path)
	if err != nil {
		t.Skipf("tape file not available: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries")
	}
}
