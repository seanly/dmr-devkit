package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/tape"
)

// CompactQuality rates how well a summary preserves critical task state.
type CompactQuality int

const (
	CompactQualityUnknown CompactQuality = iota
	CompactQualityGood
	CompactQualityFair
	CompactQualityPoor
)

// evaluateCompactSummary performs a lightweight heuristic quality check on the summary.
// It returns Good if the summary is non-empty and reasonably long, Fair for short summaries,
// and Poor for empty or very short summaries.
func evaluateCompactSummary(summary string) CompactQuality {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return CompactQualityPoor
	}
	if len(summary) < 20 {
		return CompactQualityPoor
	}
	if len(summary) < 100 {
		return CompactQualityFair
	}
	return CompactQualityGood
}

// CompactTape compacts the current tape context into a summary and returns the summary text.
func (a *Agent) CompactTape(ctx context.Context, tapeName string) (string, error) {
	return a.compact(ctx, tapeName, "", "manual")
}

// CompactTapeWithName compacts with a specific anchor name (used by auto-handoff).
func (a *Agent) CompactTapeWithName(ctx context.Context, tapeName, anchorName, reason string) error {
	_, err := a.compact(ctx, tapeName, anchorName, reason)
	return err
}

// compactSummaryVersion returns the configured compact_summary schema version,
// falling back to the tape default if unset.
func (a *Agent) compactSummaryVersion() int {
	version := a.compactCfg().CompactSummaryVersion
	if version <= 0 {
		version = tape.CompactSummarySchemaVersion
	}
	return version
}

type CompactSummaryStats struct {
	OriginalTokens  int
	OptimizedTokens int
	Strategy        string
}

// generateCompactSummary produces a summary and evaluates its quality.
// It returns the summary text, the heuristic quality rating, and token stats.
func (a *Agent) generateCompactSummary(ctx context.Context, tapeName string) (string, CompactQuality, CompactSummaryStats, error) {
	var stats CompactSummaryStats
	summarizer := a.buildSummarizer(tapeName)
	summary, sstats, err := summarizer(ctx, nil)
	if err != nil {
		return "", CompactQualityUnknown, stats, err
	}
	stats = sstats

	quality := evaluateCompactSummary(summary)
	return summary, quality, stats, nil
}

func (a *Agent) compact(ctx context.Context, tapeName, anchorName, triggerReason string) (string, error) {
	slog.Info("compact: starting summarization", "tape", tapeName, "reason", triggerReason)

	if !a.llmCompactEnabled() {
		if err := a.hooks.OnContextReset(ctx, tapeName, "compact"); err != nil {
			slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "compact", "error", err)
		}
		a.recordCompactEvent(tapeName, false, 0, CompactQualityUnknown, triggerReason, CompactSummaryStats{})
		slog.Info("compact: skipped LLM summary (minimal profile)")
		return "", nil
	}

	// L5/L6: prefer locally-assembled session memory so the common case does not
	// require an LLM call at all.
	if mem := a.sessionMemoryForTape(tapeName); mem != nil {
		if summary, tokens, ok := a.trySessionMemoryCompaction(mem); ok {
			quality := evaluateCompactSummary(summary)
			stats := CompactSummaryStats{Strategy: "session_memory", OptimizedTokens: tokens}
			slog.Info("compact: using session-memory summary (no LLM call)",
				"tape", tapeName, "chars", len(summary), "quality", quality.String())
			return a.writeCompactEntries(ctx, tapeName, anchorName, triggerReason, summary, quality, stats)
		}
		// Session memory present but too sparse; fall back to LLM unless disabled.
		if !a.config.AgentPolicy.Context.SessionMemoryFallback {
			slog.Info("compact: session memory insufficient and fallback disabled", "tape", tapeName)
			quality := CompactQualityUnknown
			stats := CompactSummaryStats{Strategy: "session_memory_skipped"}
			return a.writeCompactEntries(ctx, tapeName, anchorName, triggerReason, "", quality, stats)
		}
	}

	summary, quality, stats, err := a.generateCompactSummary(ctx, tapeName)
	if err != nil {
		slog.Error("compact: summarization failed", "error", err)
		return "", err
	}

	return a.writeCompactEntries(ctx, tapeName, anchorName, triggerReason, summary, quality, stats)
}

// trySessionMemoryCompaction assembles the tape's SessionMemory into a
// structured summary. It returns the summary text, an estimated token count,
// and ok=true when the memory is rich enough to use directly (no LLM call).
func (a *Agent) trySessionMemoryCompaction(mem *SessionMemory) (string, int, bool) {
	if mem == nil || mem.IsEmpty() {
		return "", 0, false
	}
	summary, tokens := mem.assembleSummary(minSessionMemoryChars, maxSessionMemoryChars, 3)
	if summary == "" {
		return "", 0, false
	}
	return summary, tokens, true
}

// writeCompactEntries persists a compact summary (or a state-only fallback when
// the summary is poor/empty), updates caches and session memory, and notifies
// hooks. Shared by the session-memory and LLM summarization paths.
func (a *Agent) writeCompactEntries(
	ctx context.Context,
	tapeName, anchorName, triggerReason, summary string,
	quality CompactQuality,
	stats CompactSummaryStats,
) (string, error) {
	skipSummary := false
	anchorState := map[string]any{}
	if quality == CompactQualityPoor && a.config.AgentPolicy.Context.QualityFallback {
		skipSummary = true
		if fb := a.qualityFallbackKeepBefore(); fb > 0 {
			anchorState["fallback_keep_before"] = fb
		}
		slog.Warn("compact: summary quality is poor, falling back to raw-message retention", "tape", tapeName)
	}

	a.recordCompactEvent(tapeName, !skipSummary, len(summary), quality, triggerReason, stats)

	entries, err := a.tape.Compact(ctx, tape.CompactOpts{
		Tape:           tapeName,
		AnchorName:     anchorName,
		SummaryVersion: a.compactSummaryVersion(),
		Summary:        summary,
		Quality:        quality.String(),
		SkipSummary:    skipSummary,
		AnchorState:    anchorState,
	})
	if err != nil {
		slog.Error("compact: failed to write compact entries", "error", err)
		return "", err
	}

	// A successful compact starts a fresh memory cycle.
	a.resetSessionMemory(tapeName)

	// Re-attach a minimal working environment (recent files, discovered tools)
	// so the model does not start the post-compact turn from an empty context.
	a.rebuildPostCompactContext(ctx, tapeName)

	for _, e := range entries {
		if e.Kind != "compact_summary" {
			continue
		}
		if c, ok := e.Payload["content"].(string); ok {
			// Cache the freshly written summary so the next compaction can reuse
			// it as inherited context without scanning the tape again.
			a.setLatestSummaryCache(tapeName, e.ID, c)
			slog.Info("compact: summary generated", "chars", len(c), "quality", quality.String(), "summary", c)
			if err := a.hooks.OnContextReset(ctx, tapeName, "compact"); err != nil {
				slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "compact", "error", err)
			}
			return c, nil
		}
	}
	if skipSummary {
		if err := a.hooks.OnContextReset(ctx, tapeName, "compact"); err != nil {
			slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "compact", "error", err)
		}
	}
	return "", nil
}

func (a *Agent) buildSummarizer(tapeName string) func(ctx context.Context, messages []map[string]any) (string, CompactSummaryStats, error) {
	return func(ctx context.Context, messages []map[string]any) (string, CompactSummaryStats, error) {
		strategy := a.resolveContextStrategy()
		stats := CompactSummaryStats{Strategy: strategy.String()}
		// Optimize messages before sending to LLM. Prefer the raw tape entries so we
		// can identify compact_summary by kind; fall back to the message
		// stream passed by tape.Compact when no store is available (tests).
		var optimized []map[string]any
		if a.tape != nil && a.tape.Store != nil {
			entries, err := a.tape.Store.FetchAll(tapeName, &tape.FetchOpts{LastAnchor: true})
			if err != nil {
				entries, err = a.tape.Store.FetchAll(tapeName, nil)
			}
			if err == nil && len(entries) > 0 {
				cachedID, cachedContent := a.latestSummaryCache(tapeName)
				optimized = optimizeEntriesForSummaryWithCache(entries, cachedID, cachedContent)
			}
		}
		if optimized == nil {
			optimized = optimizeMessagesForSummary(messages)
		}

		originalCount := len(optimized)
		originalSize := calculateMessagesSize(optimized)

		// Estimate tokens using the new token estimator
		estimator := NewTokenEstimator()
		originalTokens := estimator.Estimate(optimized)
		stats.OriginalTokens = originalTokens

		// Fit the summarizer input into the current tape model's context budget.
		maxInputTokens := summarizerInputBudget(a.GetCurrentModel(tapeName), a.config.AgentPolicy)
		optimized = truncateMessagesForSummary(optimized, maxInputTokens)

		optimizedCount := len(optimized)
		optimizedSize := calculateMessagesSize(optimized)
		optimizedTokens := estimator.Estimate(optimized)
		stats.OptimizedTokens = optimizedTokens

		if originalCount > 0 {
			slog.Info("compact: optimized messages",
				"original_count", originalCount, "optimized_count", optimizedCount,
				"original_bytes", originalSize, "optimized_bytes", optimizedSize,
				"original_tokens", originalTokens, "optimized_tokens", optimizedTokens,
				"max_input_tokens", maxInputTokens)
		}

		// Flatten all messages into a single user message
		flattenedContent := flattenMessagesForSummary(optimized)

		// Use structured prompt (Claude Code style)
		prompt := structuredCompactPrompt

		// Use the tape's current chat client so the summarizer benefits from the same
		// model/context-window that the agent is using. Fall back to the default client.
		chatClient := a.summarizerChatClient(tapeName)
		if chatClient == nil {
			return "", stats, fmt.Errorf("compact: no chat client available for summarization")
		}

		// Send as a single user message containing all conversation content + prompt
		resp, err := chatClient.ChatRaw(ctx, client.ChatOpts{
			Prompt:       flattenedContent + "\n\n=== 总结任务 ===\n\n" + prompt,
			Messages:     nil, // No messages array, everything is in Prompt
			SystemPrompt: "You are a professional conversation summarizer. Your task is to generate detailed, accurate, structured conversation summaries that preserve all critical technical information. Output only the content inside the <summary> tags. Do not include <analysis> tags, markdown fences, or any explanations outside the summary.",
			MaxTokens:    8000,
			ContextLimit: a.compactContextLimit(tapeName),
		})

		// If the summarizer itself overflows, retry with a much smaller input window.
		// This can happen when the configured context limit does not match the actual
		// provider limit. Each retry halves the budget and drops older messages.
		for attempt := 0; err != nil && isContextOverflowError(err) && attempt < 2 && maxInputTokens > 4000; attempt++ {
			maxInputTokens /= 2
			slog.Warn("compact: summarizer context overflow, retrying with smaller input",
				"tape", tapeName, "attempt", attempt+1, "max_input_tokens", maxInputTokens)
			optimized = truncateMessagesForSummary(optimized, maxInputTokens)
			flattenedContent = flattenMessagesForSummary(optimized)
			stats.OptimizedTokens = estimator.Estimate(optimized)
			resp, err = chatClient.ChatRaw(ctx, client.ChatOpts{
				Prompt:       flattenedContent + "\n\n=== 总结任务 ===\n\n" + prompt,
				Messages:     nil,
				SystemPrompt: "You are a professional conversation summarizer. Your task is to generate detailed, accurate, structured conversation summaries that preserve all critical technical information. Output only the content inside the <summary> tags. Do not include <analysis> tags, markdown fences, or any explanations outside the summary.",
				MaxTokens:    8000,
				ContextLimit: a.compactContextLimit(tapeName),
			})
		}

		if err != nil {
			return "", stats, err
		}

		rawResp := resp.Text
		if strings.TrimSpace(rawResp) == "" && strings.TrimSpace(resp.Reasoning) != "" {
			slog.Warn("compact: model returned empty text, falling back to reasoning content")
			rawResp = resp.Reasoning
		}

		// Extract summary from the response
		// The model should output the content wrapped in <summary>...</summary> tags.
		// We extract only the <summary> part for storage.
		summary := extractSummaryTag(rawResp)

		// If no summary tag was found, use the raw response (fallback)
		if summary == rawResp && !hasSummaryTag(rawResp) {
			slog.Warn("compact: model did not produce structured output, using raw response")
		}

		if strings.TrimSpace(summary) == "" {
			return "", stats, fmt.Errorf("compact: model produced an empty summary")
		}

		return summary, stats, nil
	}
}

// summarizerChatClient returns the chat client that should be used for compact
// summarization for the given tape. It prefers the tape's current model override
// and falls back to the agent default.
func (a *Agent) summarizerChatClient(tapeName string) *client.ChatClient {
	if cc := a.getChatClient(tapeName); cc != nil {
		return cc
	}
	return a.defaultChat
}


// summarizerInputBudget returns the maximum number of prompt tokens that should
// be sent to the summarizer model. It reserves room for the summarizer prompt
// itself and the generated summary output.
func summarizerInputBudget(m *config.ModelConfig, agentCfg config.AgentConfig) int {
	limit := 0
	if m != nil {
		limit = m.ResolveContextLimit(agentCfg)
	}
	if limit <= 0 {
		limit = agentCfg.MaxToken
	}
	if limit <= 0 {
		// Unknown model: assume a modern large-context default (e.g. Claude Sonnet).
		return 120_000
	}

	// Reserve tokens for the summarizer instructions and the generated summary.
	const reserved = 9000
	if limit > reserved+4000 {
		return limit - reserved
	}
	// Small configured budget: allow at least half for input.
	return limit / 2
}

// qualityFallbackKeepBefore returns the number of raw messages to retain before a
// poor-quality compact anchor when QualityFallback is enabled. Explicit config
// wins; otherwise we double KeepBeforeAnchor (with a floor and cap).
func (a *Agent) qualityFallbackKeepBefore() int {
	ctxCfg := a.config.AgentPolicy.Context
	if !ctxCfg.QualityFallback {
		return 0
	}
	if ctxCfg.QualityFallbackKeepBefore > 0 {
		return ctxCfg.QualityFallbackKeepBefore
	}
	keep := ctxCfg.KeepBeforeAnchor * 2
	if keep <= 0 {
		keep = 6
	}
	if keep > 12 {
		keep = 12
	}
	return keep
}
