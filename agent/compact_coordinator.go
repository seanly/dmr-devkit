package agent

import (
	"log/slog"

	"github.com/seanly/dmr-devkit/config"
)

// compactTrigger classifies the source of a compact request so the coordinator
// can apply different rules to voluntary vs. forced compacts.
type compactTrigger int

const (
	triggerPreemptive compactTrigger = iota // estimate-based, before the LLM call
	triggerProactive                        // usage-based, after tool execution
	triggerReactive                         // forced by a context-overflow API error
	triggerManual                           // user / tool initiated
)

func (t compactTrigger) String() string {
	switch t {
	case triggerPreemptive:
		return "preemptive"
	case triggerProactive:
		return "proactive"
	case triggerReactive:
		return "reactive"
	case triggerManual:
		return "manual"
	default:
		return "unknown"
	}
}

// CompactCoordinator unifies all compact trigger decisions for a single tape.
//
// It replaces the previously scattered lastCompactStep checks with a single
// gate that enforces:
//  1. A cooldown gap between voluntary (preemptive/proactive) compacts.
//  2. A per-cycle max-compacts cap that forces lighter degradation (snip /
//     microcompact) instead of "summary of a summary of a summary".
//  3. A pressure override that allows a compact sooner when estimated tokens
//     have already crossed the configured threshold.
//
// Reactive (overflow) and manual compacts bypass the cap because they are
// either forced by the provider or explicitly requested by the user; the
// loop-level autoHandoffDone guard already prevents reactive loops.
//
// All methods assume the caller holds the owning tapeState mutex.
type CompactCoordinator struct {
	// lastCompactStep is the loop step of the most recent compact (-1 = never).
	lastCompactStep int
	// compactCount is the number of voluntary compacts in the current cycle.
	compactCount int
	// snipAttempted records whether snip was attempted for the current turn so
	// we do not repeat the same degradation twice in one step.
	snipAttempted bool
}

func newCompactCoordinator() CompactCoordinator {
	return CompactCoordinator{lastCompactStep: -1}
}

// ShouldCompact reports whether a compact of the given trigger kind is
// permitted right now. estimatedTokens/limit/threshold drive the pressure
// override; pass zeros when unknown.
func (c *CompactCoordinator) ShouldCompact(
	step, estimatedTokens, limit int,
	threshold float64,
	cfg config.ContextConfig,
	trigger compactTrigger,
) bool {
	// Manual compacts are always honored.
	if trigger == triggerManual {
		return true
	}
	// Reactive compacts are forced; the loop guards against repeats.
	if trigger == triggerReactive {
		return true
	}

	// New conversation cycle (step counter wrapped): reset and allow.
	hasCompacted := c.lastCompactStep >= 0
	if hasCompacted && step < c.lastCompactStep {
		c.lastCompactStep = 0
		c.compactCount = 0
		c.snipAttempted = false
		return true
	}

	// Per-cycle cap on voluntary compacts: force lighter degradation instead.
	maxCompacts := cfg.MaxCompactsPerAnchor
	if maxCompacts > 0 && c.compactCount >= maxCompacts {
		slog.Debug("compact: coordinator rejected (max compacts reached)",
			"step", step, "count", c.compactCount, "max", maxCompacts, "trigger", trigger)
		return false
	}

	gap := 0
	if hasCompacted {
		gap = step - c.lastCompactStep
	}

	cooldown := cfg.CompactGap
	if cooldown <= 0 {
		cooldown = 3
	}
	pressureGap := cfg.PressureOverrideGap
	if pressureGap <= 0 {
		pressureGap = 1
	}

	// Normal rule: allow if never compacted or cooldown has elapsed.
	if !hasCompacted || gap >= cooldown {
		return true
	}

	// Pressure override: tokens already past the threshold → allow sooner.
	if limit > 0 && estimatedTokens > 0 && gap >= pressureGap {
		if float64(estimatedTokens) >= float64(limit)*threshold {
			return true
		}
	}
	return false
}

// RecordCompact registers that a compact was performed at the given step.
func (c *CompactCoordinator) RecordCompact(step int, trigger compactTrigger) {
	c.lastCompactStep = step
	c.snipAttempted = false
	if trigger == triggerPreemptive || trigger == triggerProactive {
		c.compactCount++
	}
}

// ResetForNewCycle clears the coordinator for a fresh conversation cycle.
func (c *CompactCoordinator) ResetForNewCycle() {
	c.lastCompactStep = -1
	c.compactCount = 0
	c.snipAttempted = false
}

// MarkSnipAttempted records that a snip was attempted this turn.
func (c *CompactCoordinator) MarkSnipAttempted() {
	c.snipAttempted = true
}

// SnipAttempted reports whether snip was already attempted this turn.
func (c *CompactCoordinator) SnipAttempted() bool {
	return c.snipAttempted
}

// LastCompactStep returns the step of the most recent compact (-1 if none).
func (c *CompactCoordinator) LastCompactStep() int {
	return c.lastCompactStep
}

// CompactCount returns the number of voluntary compacts in the current cycle.
func (c *CompactCoordinator) CompactCount() int {
	return c.compactCount
}
