package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/seanly/dmr-devkit/config"
)

// ReviewDelegate runs a named critic (typically a skill-backed subagent) after tool rounds.
type ReviewDelegate interface {
	RunCritic(ctx context.Context, tapeName, skillName, task string) (output string, critical bool, err error)
}

func (a *Agent) getReviewDelegate() ReviewDelegate {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.reviewRunner
}

// SetReviewDelegate wires skill-backed critics for [config.ReviewConfig].Chain.
func (a *Agent) SetReviewDelegate(d ReviewDelegate) {
	a.cfgMu.Lock()
	a.reviewRunner = d
	a.cfgMu.Unlock()
}

func (a *Agent) reviewCfg() config.ReviewConfig {
	return a.config.AgentPolicy.Review
}

func (a *Agent) shouldReviewTool(toolName string) bool {
	cfg := a.reviewCfg()
	if !cfg.Enabled {
		return false
	}
	for _, t := range cfg.AfterTools {
		if t == toolName {
			return true
		}
	}
	for _, pat := range cfg.AfterToolPatterns {
		if strings.Contains(toolName, pat) {
			return true
		}
	}
	return false
}

func formatReviewTask(toolNames []string, results []any) string {
	var b strings.Builder
	b.WriteString("Review the latest tool round.\nTools: ")
	b.WriteString(strings.Join(toolNames, ", "))
	b.WriteString("\nResults:\n")
	for i, r := range results {
		name := "tool"
		if i < len(toolNames) {
			name = toolNames[i]
		}
		fmt.Fprintf(&b, "- %s: %s\n", name, truncateReviewText(fmt.Sprint(r), 1500))
	}
	b.WriteString("\nReply PASS unless you find a critical issue. Prefix critical findings with [CRITICAL].")
	return b.String()
}

func truncateReviewText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseCriticalVerdict(output string) bool {
	up := strings.ToUpper(strings.TrimSpace(output))
	if strings.Contains(up, "[CRITICAL]") {
		return true
	}
	if strings.HasPrefix(up, "CRITICAL:") || strings.HasPrefix(up, "CRITICAL ") {
		return true
	}
	return false
}

func formatReviewFeedback(criticOutput string) string {
	out := strings.TrimSpace(criticOutput)
	if out == "" {
		return "[Review] A critical issue was found. Re-read the critic feedback and adjust before continuing."
	}
	return "[Review — action required before continuing]\n" + out
}

// ReviewResult is returned after a post-tool review chain run.
type ReviewResult struct {
	Feedback string
	HardStop bool // skip tool-round continuation and re-enter LLM with review feedback
}

// runPostToolReview executes configured review chain critics after selected tool rounds.
// When BlockOnCritical triggers, returns feedback for tape injection and HardStop=true.
func (a *Agent) runPostToolReview(ctx context.Context, tapeName string, step int, toolNames []string, results []any) ReviewResult {
	cfg := a.reviewCfg()
	if !cfg.Enabled {
		return ReviewResult{}
	}
	reviewed := false
	for _, name := range toolNames {
		if a.shouldReviewTool(name) {
			reviewed = true
			break
		}
	}
	if !reviewed {
		return ReviewResult{}
	}

	a.recordLoopEvent(tapeName, "loop:review", map[string]any{
		"step": step, "tools": toolNames, "chain": cfg.Chain,
	})

	task := formatReviewTask(toolNames, results)
	delegate := a.getReviewDelegate()
	maxDepth := cfg.MaxChainDepth
	if maxDepth <= 0 {
		maxDepth = len(cfg.Chain)
	}
	var result ReviewResult
	for i, skill := range cfg.Chain {
		if i >= maxDepth {
			break
		}
		if delegate == nil {
			break
		}
		out, critical, err := delegate.RunCritic(ctx, tapeName, skill, task)
		ev := map[string]any{"step": step, "skill": skill, "index": i, "critical": critical}
		if err != nil {
			ev["error"] = err.Error()
			a.recordLoopEvent(tapeName, "loop:review_step", ev)
			slog.Warn("review: critic failed", "skill", skill, "error", err)
			continue
		}
		if len(out) > 0 {
			ev["output_chars"] = len(out)
		}
		a.recordLoopEvent(tapeName, "loop:review_step", ev)
		if critical {
			if cfg.BlockOnCritical {
				result.Feedback = formatReviewFeedback(out)
				result.HardStop = true
				a.recordLoopEvent(tapeName, "loop:review_blocked", map[string]any{
					"step": step, "skill": skill, "index": i,
				})
				break
			}
		}
	}

	_ = a.hooks.AfterToolRound(ctx, AfterToolRoundArgs{
		TapeName: tapeName, Step: step, ToolNames: toolNames, ToolResults: results,
	})

	if result.HardStop {
		slog.Warn("review: blocked on critical finding", "tape", tapeName, "step", step)
	}
	return result
}
