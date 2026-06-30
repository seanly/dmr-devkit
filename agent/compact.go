package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
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

	if !a.llmCompactEnabled() {
		// Even without LLM summarization, a handoff is a context reset.
		if a.clearToolsOnCompact() {
			a.ClearDiscoveredTools(tapeName)
			if err := a.hooks.OnContextReset(ctx, tapeName, "handoff"); err != nil {
				slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "handoff", "error", err)
			}
		}
		slog.Info("compact: skipped LLM summary (minimal profile)")
		return "", nil
	}

	entries, err := a.tape.Compact(ctx, tape.CompactOpts{
		Tape:       tapeName,
		AnchorName: "handoff/tool",
		EventName:  "handoff/tool",
		Summarizer: a.buildSummarizer(tapeName, focus),
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
			// Only reset plugin/discovered-tool state after a successful compact.
			if a.clearToolsOnCompact() {
				a.ClearDiscoveredTools(tapeName)
				if err := a.hooks.OnContextReset(ctx, tapeName, "handoff"); err != nil {
					slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "handoff", "error", err)
				}
			}
			return c, nil
		}
	}
	return "", nil
}

func (a *Agent) compact(ctx context.Context, tapeName, anchorName, focus string) (string, error) {
	slog.Info("compact: starting summarization", "tape", tapeName)

	if !a.llmCompactEnabled() {
		// Even without LLM summarization, a compact is a context reset.
		if a.clearToolsOnCompact() {
			a.ClearDiscoveredTools(tapeName)
			if err := a.hooks.OnContextReset(ctx, tapeName, "compact"); err != nil {
				slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "compact", "error", err)
			}
		}
		slog.Info("compact: skipped LLM summary (minimal profile)")
		return "", nil
	}

	entries, err := a.tape.Compact(ctx, tape.CompactOpts{
		Tape:       tapeName,
		AnchorName: anchorName,
		Summarizer: a.buildSummarizer(tapeName, focus),
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
			// Only reset plugin/discovered-tool state after a successful compact.
			if a.clearToolsOnCompact() {
				a.ClearDiscoveredTools(tapeName)
				if err := a.hooks.OnContextReset(ctx, tapeName, "compact"); err != nil {
					slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "compact", "error", err)
				}
			}
			return c, nil
		}
	}
	return "", nil
}

func (a *Agent) buildSummarizer(tapeName, focus string) func(ctx context.Context, messages []map[string]any) (string, error) {
	return func(ctx context.Context, messages []map[string]any) (string, error) {
		// Optimize messages before sending to LLM. Prefer the raw tape entries so we
		// can identify compact_summary/task_state by kind; fall back to the message
		// stream passed by tape.Compact when no store is available (tests).
		var optimized []map[string]any
		if a.tape != nil && a.tape.Store != nil {
			entries, err := a.tape.Store.FetchAll(tapeName, &tape.FetchOpts{LastAnchor: true})
			if err != nil {
				entries, err = a.tape.Store.FetchAll(tapeName, nil)
			}
			if err == nil && len(entries) > 0 {
				optimized = optimizeEntriesForSummary(entries)
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

		// Fit the summarizer input into the current tape model's context budget.
		maxInputTokens := summarizerInputBudget(a.GetCurrentModel(tapeName), a.config.AgentPolicy)
		optimized = truncateMessagesForSummary(optimized, maxInputTokens)

		optimizedCount := len(optimized)
		optimizedSize := calculateMessagesSize(optimized)
		optimizedTokens := estimator.Estimate(optimized)

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
		if focus != "" {
			prompt += "\n\nIMPORTANT: The user has requested that the summary focus on the following topic. Prioritize information related to this focus, but still preserve other critical technical details.\nFocus: " + focus
		}

		// Use the tape's current chat client so the summarizer benefits from the same
		// model/context-window that the agent is using. Fall back to the default client.
		chatClient := a.summarizerChatClient(tapeName)
		if chatClient == nil {
			return "", fmt.Errorf("compact: no chat client available for summarization")
		}

		// Send as a single user message containing all conversation content + prompt
		resp, err := chatClient.ChatRaw(ctx, client.ChatOpts{
			Prompt:       flattenedContent + "\n\n=== 总结任务 ===\n\n" + prompt,
			Messages:     nil, // No messages array, everything is in Prompt
			SystemPrompt: "You are a professional conversation summarizer. Your task is to generate detailed, accurate, structured conversation summaries that preserve all critical technical information. Output only the content inside the <summary> tags. Do not include <analysis> tags, markdown fences, or any explanations outside the summary.",
			MaxTokens:    8000,
			ContextLimit: a.handoffContextLimit(tapeName),
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
			resp, err = chatClient.ChatRaw(ctx, client.ChatOpts{
				Prompt:       flattenedContent + "\n\n=== 总结任务 ===\n\n" + prompt,
				Messages:     nil,
				SystemPrompt: "You are a professional conversation summarizer. Your task is to generate detailed, accurate, structured conversation summaries that preserve all critical technical information. Output only the content inside the <summary> tags. Do not include <analysis> tags, markdown fences, or any explanations outside the summary.",
				MaxTokens:    8000,
				ContextLimit: a.handoffContextLimit(tapeName),
			})
		}

		if err != nil {
			return "", err
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

		// Log analysis for debugging (optional)
		analysis := extractAnalysisTag(rawResp)
		if analysis != "" {
			slog.Debug("compact: analysis section", "chars", len(analysis))
		}

		if strings.TrimSpace(summary) == "" {
			return "", fmt.Errorf("compact: model produced an empty summary")
		}

		return summary, nil
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
