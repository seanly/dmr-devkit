package agent

import (
	"log/slog"
)

// tryLightweightOverflowRecovery attempts an L7 lightweight recovery before
// falling back to a full LLM compact: it schedules an aggressive snip +
// microcompact pass for the next context build and asks the loop to retry the
// LLM call. It returns true when a retry was scheduled.
//
// It is bounded by reactiveSnipAttempts so a persistently overflowing context
// escalates to a full compact (and the loop's autoHandoffDone guard) rather
// than looping forever.
func (a *Agent) tryLightweightOverflowRecovery(tapeName string) bool {
	if !a.config.AgentPolicy.Context.SnipEnabled {
		return false
	}
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	const maxLightweightAttempts = 1
	if ts.reactiveSnipAttempts >= maxLightweightAttempts {
		return false
	}
	ts.reactiveSnipAttempts++
	ts.reactiveSnipPending = true
	slog.Info("reactive: lightweight recovery scheduled (snip + microcompact)",
		"tape", tapeName, "attempt", ts.reactiveSnipAttempts)
	return true
}
