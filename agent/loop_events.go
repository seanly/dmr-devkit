package agent

import (
	"context"
	"log/slog"

	"github.com/seanly/dmr-devkit/tape"
)

func (a *Agent) recordLoopEvent(tapeName, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	_ = a.tape.AppendEntry(tapeName, tape.NewEventEntry(name, data))
}

func (a *Agent) recordStepStart(tapeName, execID string, step, tokensEst, toolsVisible int) {
	a.recordLoopEvent(tapeName, "loop:step_start", map[string]any{
		"step": step, "exec_id": execID, "tokens_est": tokensEst, "tools_visible_count": toolsVisible,
	})
}

func (a *Agent) recordToolRound(tapeName string, step int, tools []string, denyCount int) {
	a.recordLoopEvent(tapeName, "loop:tool_round", map[string]any{
		"step": step, "tools": tools, "deny_count": denyCount,
	})
}

func (a *Agent) recordHandoffEvent(tapeName, reason, anchor string, stateEntryID int, compactAttempted bool) {
	a.recordLoopEvent(tapeName, "loop:handoff", map[string]any{
		"reason": reason, "anchor": anchor, "state_entry_id": stateEntryID,
		"compact_attempted": compactAttempted,
	})
}

func (a *Agent) recordCompactEvent(tapeName string, success bool, summaryChars int, judgePass *bool, quality CompactQuality, triggerReason string, stats CompactSummaryStats) {
	data := map[string]any{
		"success":                success,
		"summary_chars":          summaryChars,
		"quality":                quality.String(),
		"trigger_reason":         triggerReason,
		"original_tokens":        stats.OriginalTokens,
		"optimized_tokens":       stats.OptimizedTokens,
		"strategy":               stats.Strategy,
		"rolling_summary":        stats.RollingSummary,
		"rolling_full_refresh":   stats.RollingFullRefresh,
		"rolling_count":          stats.RollingCount,
		"summarized_since_id":    stats.SummarizedSinceID,
		"previous_summary_chars": stats.PreviousSummaryChars,
		"semantic_collapse":      stats.SemanticCollapse,
	}
	if judgePass != nil {
		data["judge_pass"] = *judgePass
	}
	a.recordLoopEvent(tapeName, "loop:compact", data)
}

func (q CompactQuality) String() string {
	switch q {
	case CompactQualityGood:
		return "good"
	case CompactQualityFair:
		return "fair"
	case CompactQualityPoor:
		return "poor"
	case CompactQualityUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

func (a *Agent) recordRunEnd(tapeName string, step, toolIterations, promptTokens int) {
	a.recordLoopEvent(tapeName, "loop:run_end", map[string]any{
		"steps": step, "tool_iterations": toolIterations, "prompt_tokens": promptTokens,
	})
}

// performContextHandoff snapshots task state then runs compact or anchor-only fallback.
func (a *Agent) performContextHandoff(ctx context.Context, tapeName, handoffName, reason string, step int) (compactOK bool, stateEntryID int) {
	stateEntryID = a.snapshotTaskStateBeforeHandoff(tapeName, step)
	h := a.handoffCfg()
	if !h.CompactAfterState {
		a.Handoff(tapeName, handoffName, map[string]any{"reason": reason})
		a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, false)
		return false, stateEntryID
	}
	if !a.llmCompactEnabled() {
		a.Handoff(tapeName, handoffName, map[string]any{"reason": reason, "state_only": true, "profile": "minimal"})
		a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, false)
		return false, stateEntryID
	}
	err := a.CompactTapeWithName(ctx, tapeName, handoffName, reason)
	if err != nil {
		slog.Error("compact: handoff compact failed", "reason", reason, "error", err)
		if h.CompactRequired {
			a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, true)
			return false, stateEntryID
		}
		a.Handoff(tapeName, handoffName, map[string]any{
			"reason": reason, "compact_error": err.Error(), "state_only": true,
		})
		a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, true)
		return false, stateEntryID
	}
	a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, true)
	return true, stateEntryID
}
