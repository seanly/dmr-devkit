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

func (a *Agent) recordCompactEvent(tapeName string, success bool, summaryChars int, judgePass *bool) {
	data := map[string]any{"success": success, "summary_chars": summaryChars}
	if judgePass != nil {
		data["judge_pass"] = *judgePass
	}
	a.recordLoopEvent(tapeName, "loop:compact", data)
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
		a.recordCompactEvent(tapeName, false, 0, nil)
		return false, stateEntryID
	}
	err := a.CompactTapeWithName(ctx, tapeName, handoffName)
	if err != nil {
		slog.Error("compact: handoff compact failed", "reason", reason, "error", err)
		if h.CompactRequired {
			a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, true)
			a.recordCompactEvent(tapeName, false, 0, nil)
			return false, stateEntryID
		}
		a.Handoff(tapeName, handoffName, map[string]any{
			"reason": reason, "compact_error": err.Error(), "state_only": true,
		})
		a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, true)
		a.recordCompactEvent(tapeName, false, 0, nil)
		return false, stateEntryID
	}
	a.recordHandoffEvent(tapeName, reason, handoffName, stateEntryID, true)
	a.recordCompactEvent(tapeName, true, 0, nil)
	return true, stateEntryID
}
