package agent

import (
	"context"
	"log/slog"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/handoff"
	"github.com/seanly/dmr-devkit/tape"
)

// CompactTape compacts the current tape context into a summary and returns the summary text.
func (a *Agent) CompactTape(ctx context.Context, tapeName string) (string, error) {
	return a.compact(ctx, tapeName, "", "")
}

// CompactTapeWithName compacts with a specific anchor name (used by auto-handoff).
func (a *Agent) CompactTapeWithName(ctx context.Context, tapeName, anchorName string) error {
	_, err := a.compact(ctx, tapeName, anchorName, "")
	return err
}

// CompactTapeWithFocus compacts the current tape context into a summary focused on
// the given topic. It writes a handoff/tool anchor + compact_summary + handoff/tool event.
func (a *Agent) CompactTapeWithFocus(ctx context.Context, tapeName, focus string) (string, error) {
	slog.Info("compact: starting focused summarization", "tape", tapeName, "focus", focus)

	if a.clearToolsOnCompact() {
		a.ClearDiscoveredTools(tapeName)
		if err := a.hooks.OnContextReset(ctx, tapeName, "handoff"); err != nil {
			slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "handoff", "error", err)
		}
	}

	if !a.llmCompactEnabled() {
		slog.Info("compact: skipped LLM summary (minimal profile)")
		return "", nil
	}

	entries, err := a.tape.Compact(ctx, tape.CompactOpts{
		Tape:       tapeName,
		AnchorName: "handoff/tool",
		EventName:  "handoff/tool",
		Summarizer: a.buildSummarizer(focus),
	})
	if err != nil {
		slog.Error("compact: focused summarization failed", "error", err)
		return "", err
	}
	for _, e := range entries {
		if e.Kind != "compact_summary" {
			continue
		}
		if c, ok := e.Payload["content"].(string); ok {
			slog.Info("compact: focused summary generated", "chars", len(c), "summary", c)
			if a.summaryJudgeEnabled() {
				entries, _ := a.tape.Store.FetchAll(tapeName, nil)
				st := handoff.LatestState(entries)
				pass := validateCompactSummary(st, c)
				judgePass := pass
				a.recordCompactEvent(tapeName, true, len(c), &judgePass)
				if !pass {
					slog.Warn("compact: summary failed adversarial judge", "tape", tapeName)
				}
			}
			return c, nil
		}
	}
	return "", nil
}

func (a *Agent) compact(ctx context.Context, tapeName, anchorName, focus string) (string, error) {
	slog.Info("compact: starting summarization", "tape", tapeName)

	if a.clearToolsOnCompact() {
		a.ClearDiscoveredTools(tapeName)
		if err := a.hooks.OnContextReset(ctx, tapeName, "compact"); err != nil {
			slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "compact", "error", err)
		}
	}

	if !a.llmCompactEnabled() {
		slog.Info("compact: skipped LLM summary (minimal profile)")
		return "", nil
	}

	entries, err := a.tape.Compact(ctx, tape.CompactOpts{
		Tape:       tapeName,
		AnchorName: anchorName,
		Summarizer: a.buildSummarizer(focus),
	})
	if err != nil {
		slog.Error("compact: summarization failed", "error", err)
		return "", err
	}
	for _, e := range entries {
		if e.Kind != "compact_summary" {
			continue
		}
		if c, ok := e.Payload["content"].(string); ok {
			slog.Info("compact: summary generated", "chars", len(c), "summary", c)
			if a.summaryJudgeEnabled() {
				entries, _ := a.tape.Store.FetchAll(tapeName, nil)
				st := handoff.LatestState(entries)
				pass := validateCompactSummary(st, c)
				judgePass := pass
				a.recordCompactEvent(tapeName, true, len(c), &judgePass)
				if !pass {
					slog.Warn("compact: summary failed adversarial judge", "tape", tapeName)
				}
			}
			return c, nil
		}
	}
	return "", nil
}

func (a *Agent) buildSummarizer(focus string) func(ctx context.Context, messages []map[string]any) (string, error) {
	return func(ctx context.Context, messages []map[string]any) (string, error) {
		// Optimize messages before sending to LLM
		originalCount := len(messages)
		originalSize := calculateMessagesSize(messages)

		// Estimate tokens using the new token estimator
		estimator := NewTokenEstimator()
		originalTokens := estimator.Estimate(messages)

		messages = optimizeMessagesForSummary(messages)

		optimizedCount := len(messages)
		optimizedSize := calculateMessagesSize(messages)
		optimizedTokens := estimator.Estimate(messages)

		if originalCount > 0 {
			slog.Info("compact: optimized messages",
				"original_count", originalCount, "optimized_count", optimizedCount,
				"original_bytes", originalSize, "optimized_bytes", optimizedSize,
				"original_tokens", originalTokens, "optimized_tokens", optimizedTokens)
		}

		// Flatten all messages into a single user message
		flattenedContent := flattenMessagesForSummary(messages)

		// Use structured prompt (Claude Code style)
		prompt := structuredCompactPrompt
		if focus != "" {
			prompt += "\n\nIMPORTANT: The user has requested that the summary focus on the following topic. Prioritize information related to this focus, but still preserve other critical technical details.\nFocus: " + focus
		}

		// Send as a single user message containing all conversation content + prompt
		// Use default chat client for summarization (no per-tape override needed)
		rawResp, err := a.defaultChat.Chat(ctx, client.ChatOpts{
			Prompt:       flattenedContent + "\n\n=== 总结任务 ===\n\n" + prompt,
			Messages:     nil, // No messages array, everything is in Prompt
			SystemPrompt: "You are a professional conversation summarizer. Your task is to generate detailed, accurate, structured conversation summaries that preserve all critical technical information. Output only the content inside the <summary> tags. Do not include <analysis> tags, markdown fences, or any explanations outside the summary.",
			MaxTokens:    3000,
		})
		if err != nil {
			return "", err
		}

		// Extract summary from the response
		// The model should output the content wrapped in <summary>...</summary> tags.
		// We extract only the <summary> part for storage.
		summary := extractSummaryTag(rawResp)

		// If no summary tag was found, use the raw response (fallback)
		if summary == rawResp && !hasSummaryTag(rawResp) {
			slog.Warn("compact: model did not produce structured output, using raw response")
		}

		// Log analysis for debugging (optional)
		analysis := extractAnalysisTag(rawResp)
		if analysis != "" {
			slog.Debug("compact: analysis section", "chars", len(analysis))
		}

		return summary, nil
	}
}
