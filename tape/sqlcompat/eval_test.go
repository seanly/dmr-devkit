package sqlcompat

import (
	"testing"
)

// TestEvalModernc runs tape evaluation cases against modernc (CI-safe baseline).
func TestEvalModernc(t *testing.T) {
	rep := &Report{}
	t.Run("tape", func(t *testing.T) {
		RunTapeCases(t, DriverModernc, rep)
	})
	t.Run("concurrency", func(t *testing.T) {
		RunConcurrencyCases(t, DriverModernc, rep)
	})
	t.Logf("\n%s", rep.Summary())
}
