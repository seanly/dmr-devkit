package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/handoff"
)

// summaryJudgeResult is the structured output expected from the LLM judge.
type summaryJudgeResult struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

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

// validateCompactSummaryWithLLM uses the current tape model to semantically evaluate
// whether the compact summary preserves the task state. It returns the judge decision,
// an explanatory reason, and an error. When an error is returned (e.g. LLM call failure
// or unparseable output), the caller should fall back to validateCompactSummary.
func validateCompactSummaryWithLLM(
	ctx context.Context,
	chatClient *client.ChatClient,
	state *handoff.State,
	summary string,
	tapeName string,
) (bool, string, error) {
	if state == nil || strings.TrimSpace(state.Goal) == "" {
		return summary != "", "", nil
	}
	if strings.TrimSpace(summary) == "" {
		return false, "summary is empty", nil
	}

	prompt := buildSummaryJudgePrompt(state, summary)
	resp, err := chatClient.ChatRaw(ctx, client.ChatOpts{
		Prompt:       prompt,
		SystemPrompt: summaryJudgeSystemPrompt,
		MaxTokens:    500,
	})
	if err != nil {
		return false, "", fmt.Errorf("LLM judge request failed: %w", err)
	}

	raw := resp.Text
	if strings.TrimSpace(raw) == "" && strings.TrimSpace(resp.Reasoning) != "" {
		raw = resp.Reasoning
	}

	result, err := parseSummaryJudgeResponse(raw)
	if err != nil {
		return false, "", fmt.Errorf("LLM judge response unparseable: %w", err)
	}

	if result.Pass {
		slog.Debug("compact: LLM judge passed", "tape", tapeName)
	} else {
		slog.Debug("compact: LLM judge failed", "tape", tapeName, "reason", result.Reason)
	}
	return result.Pass, result.Reason, nil
}

const summaryJudgeSystemPrompt = `You are a strict but fair evaluator. Your job is to decide whether a conversation summary accurately preserves the user's task state.

You will be given the current TaskState (goal, constraints, pending items, active files) and the summary generated after a context compaction. Accept paraphrases, synonyms, and equivalent Chinese expressions; do not require the summary to contain the exact original wording.

Output ONLY a single JSON object with no markdown code fences and no extra commentary:
{"pass": true|false, "reason": "short explanation in the same language as the conversation"}`

// buildSummaryJudgePrompt renders the judge prompt from task state and summary.
func buildSummaryJudgePrompt(state *handoff.State, summary string) string {
	return fmt.Sprintf(`%s

[Summary to Evaluate]
%s

Evaluate the summary on these criteria:
1. Does it capture the user's goal and intent, including paraphrases or equivalent expressions?
2. Does it preserve the active constraints?
3. Does it preserve pending tasks?
4. Does it preserve active files and artifacts?
5. Is it free of contradictions or hallucinations?

Output ONLY JSON: {"pass": true|false, "reason": "..."}`,
		state.FormatPromptBlock(),
		summary,
	)
}

// parseSummaryJudgeResponse extracts the first JSON object from the model output and
// unmarshals it into summaryJudgeResult. It tolerates surrounding whitespace.
func parseSummaryJudgeResponse(raw string) (summaryJudgeResult, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return summaryJudgeResult{}, fmt.Errorf("empty judge response")
	}

	// Strip optional markdown code fences.
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	// Find the first '{' and last '}' to isolate a JSON object.
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return summaryJudgeResult{}, fmt.Errorf("no JSON object found in response")
	}
	text = text[start : end+1]

	var result summaryJudgeResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return summaryJudgeResult{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return result, nil
}
