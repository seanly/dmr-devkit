//go:build turso_eval

package sqlcompat

import "testing"

// TestEvalTursoNativeFTS evaluates Turso Tantivy FTS (not SQLite FTS5) against DMR search scenarios.
func TestEvalTursoNativeFTS(t *testing.T) {
	if !TursoEvalAvailable() {
		t.Fatal("turso_eval build tag set but tursogo not linked")
	}
	rep := &Report{}
	RunTursoFTSCases(t, rep)
	t.Logf("\n=== Turso native FTS (Tantivy) ===\n%s", rep.Summary())
	if rep.BlockerCount() > 0 && rep.TursoFailCount() > 0 {
		t.Fatalf("turso native FTS: blockers=%d fails=%d", rep.BlockerCount(), rep.TursoFailCount())
	}
}
