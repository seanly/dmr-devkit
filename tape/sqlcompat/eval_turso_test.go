//go:build turso_eval

package sqlcompat

import (
	"testing"
)

// TestEvalTursoCompare runs the same cases on modernc and tursogo and logs a diff summary.
func TestEvalTursoCompare(t *testing.T) {
	if !TursoEvalAvailable() {
		t.Fatal("turso_eval build tag set but tursogo driver not registered")
	}

	moderncRep := &Report{}
	t.Run("modernc", func(t *testing.T) {
		RunTapeCases(t, DriverModernc, moderncRep)
		RunConcurrencyCases(t, DriverModernc, moderncRep)
	})

	tursoRep := &Report{}
	t.Run("turso", func(t *testing.T) {
		RunTapeCases(t, DriverTurso, tursoRep)
		RunConcurrencyCases(t, DriverTurso, tursoRep)
	})

	combined := &Report{}
	combined.Results = append(combined.Results, moderncRep.Results...)
	combined.Results = append(combined.Results, tursoRep.Results...)

	t.Logf("\n=== Turso compatibility matrix ===\n%s", combined.Summary())
	t.Logf("turso blockers=%d turso_fails=%d", combined.BlockerCount(), combined.TursoFailCount())

	if combined.BlockerCount() > 0 || combined.TursoFailCount() > 0 {
		t.Fatalf("tursogo eval: blockers=%d fails=%d (see matrix above)", combined.BlockerCount(), combined.TursoFailCount())
	}
}
