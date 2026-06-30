package agent

import "sync"

// contextBudget unifies provider-reported token usage with local estimation.
// It prefers reported usage when available and falls back to the latest estimate.
type contextBudget struct {
	mu              sync.Mutex
	reportedTokens  int
	estimatedTokens int
	hasReport       bool
}

func newContextBudget() *contextBudget {
	return &contextBudget{}
}

// UpdateReported records the provider's prompt_tokens for the last LLM call.
func (b *contextBudget) UpdateReported(n int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if n >= 0 {
		b.reportedTokens = n
		b.hasReport = true
	}
}

// UpdateEstimated records the local token estimate for the current window.
func (b *contextBudget) UpdateEstimated(n int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if n >= 0 {
		b.estimatedTokens = n
	}
}

// Effective returns the best available token count: reported usage wins,
// otherwise the latest estimate. Returns 0 if nothing has been recorded.
func (b *contextBudget) Effective() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hasReport {
		return b.reportedTokens
	}
	return b.estimatedTokens
}

// Ratio returns the current budget ratio against the given limit.
// Returns 0 if limit <= 0 or Effective() == 0.
func (b *contextBudget) Ratio(limit int) float64 {
	if limit <= 0 {
		return 0
	}
	eff := b.Effective()
	if eff <= 0 {
		return 0
	}
	return float64(eff) / float64(limit)
}

// HasReport returns true if provider-reported usage has ever been recorded.
func (b *contextBudget) HasReport() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hasReport
}

// Reset clears all recorded budget state.
func (b *contextBudget) Reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reportedTokens = 0
	b.estimatedTokens = 0
	b.hasReport = false
}
