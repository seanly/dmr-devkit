package agent

import (
	"strings"

	"github.com/seanly/dmr-devkit/handoff"
)

// validateCompactSummary performs a lightweight adversarial check: the summary
// should retain the task goal (or a significant token from it).
func validateCompactSummary(state *handoff.State, summary string) bool {
	if state == nil || strings.TrimSpace(state.Goal) == "" {
		return summary != ""
	}
	if strings.TrimSpace(summary) == "" {
		return false
	}
	goal := strings.ToLower(strings.TrimSpace(state.Goal))
	summaryLower := strings.ToLower(summary)
	if strings.Contains(summaryLower, goal) {
		return true
	}
	for _, word := range strings.Fields(goal) {
		if len(word) < 4 {
			continue
		}
		if strings.Contains(summaryLower, word) {
			return true
		}
	}
	return false
}
