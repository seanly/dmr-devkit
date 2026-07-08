package agent

import (
	"log/slog"

	"github.com/seanly/dmr-devkit/tape"
)

// snipWarnMarginTokens is the safety margin below the compact threshold at
// which snip kicks in. When estimated tokens exceed (limit*threshold - margin),
// the loop tries to snip older history before triggering an LLM compact.
const snipWarnMarginTokens = 20_000

// applySnipForBudget decides whether to apply L2 history snip for the current
// step and, if so, configures tapeCtx to drop a number of safe units from the
// front of the context. It returns true when snip was applied.
//
// Snip is best-effort: it sheds tokens by dropping whole older turns so the
// LLM call fits without an expensive compact. The reactive-overflow path
// remains the backstop if snip under- or over-shoots.
func (a *Agent) applySnipForBudget(tapeName string, tapeCtx *tape.TapeContext, estimatedTokens, limit int, threshold float64) bool {
	if !a.config.AgentPolicy.Context.SnipEnabled || tapeCtx == nil {
		return false
	}
	if limit <= 0 || estimatedTokens <= 0 {
		return false
	}
	warnAt := int(float64(limit)*threshold) - snipWarnMarginTokens
	if warnAt <= 0 {
		warnAt = int(float64(limit) * 0.6)
	}
	if estimatedTokens <= warnAt {
		return false
	}

	// Estimate how many front units to drop. We approximate the average tokens
	// per message and target the warning threshold. The exact count does not
	// matter: snipFrontUnits protects the recent turns and system messages.
	target := warnAt
	overflow := estimatedTokens - target
	if overflow <= 0 {
		return false
	}
	// Rough: assume ~600 tokens per dropped unit. Clamp to a sane range.
	const approxTokensPerUnit = 600
	units := overflow / approxTokensPerUnit
	if units < 1 {
		units = 1
	}
	if units > 32 {
		units = 32
	}

	tapeCtx.SnipDropUnits = units
	tapeCtx.SnipKeepRecentTurns = 2
	slog.Info("snip: applied history snip to defer compact",
		"tape", tapeName, "estimated_tokens", estimatedTokens, "warn_at", warnAt,
		"drop_units", units)

	// Mark the coordinator so we don't repeat snip this turn.
	if ts := a.tapeStates.get(tapeName); ts != nil {
		ts.mu.Lock()
		ts.coordinator.MarkSnipAttempted()
		ts.mu.Unlock()
	}
	return true
}
