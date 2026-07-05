package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/seanly/dmr-devkit/client"
)

// summaryJudgeResult is the structured output expected from the LLM judge.
type summaryJudgeResult struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

// validateCompactSummary performs a lightweight adversarial check: the summary
// should be non-empty and contain at least a few words.
func validateCompactSummary(summary string) bool {
	return strings.TrimSpace(summary) != "" && len(strings.Fields(summary)) >= 3
}

// validateCompactSummaryWithLLM uses the current tape model to semantically evaluate
// whether the compact summary is coherent and complete. It returns the judge decision,
// an explanatory reason, and an error. When an error is returned (e.g. LLM call failure
// or unparseable output), the caller should fall back to validateCompactSummary.
func validateCompactSummaryWithLLM(
	ctx context.Context,
	chatClient *client.ChatClient,
	summary string,
	tapeName string,
) (bool, string, error) {
	if strings.TrimSpace(summary) == "" {
		return false, "summary is empty", nil
	}
	if chatClient == nil {
		return false, "chat client is nil", fmt.Errorf("chat client is nil")
	}

	prompt := buildSummaryJudgePrompt(summary)
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

const summaryJudgeSystemPrompt = `You are a strict but fair evaluator. Your job is to decide whether a conversation summary is coherent and complete.

You will be given a summary generated after a context compaction. Evaluate whether it captures the user's intent and preserves critical technical details.

Output ONLY a single JSON object with no markdown code fences and no extra commentary:
{"pass": true|false, "reason": "short explanation in the same language as the summary"}`

// buildSummaryJudgePrompt renders the judge prompt from summary.
func buildSummaryJudgePrompt(summary string) string {
	return fmt.Sprintf(`%s

[Summary to Evaluate]
%s

Evaluate the summary on these criteria:
1. Does it capture the user's goal and intent?
2. Does it preserve active constraints and requirements?
3. Does it preserve pending tasks?
4. Does it preserve active files and artifacts?
5. Is it free of contradictions or hallucinations?

Output ONLY JSON: {"pass": true|false, "reason": "..."}`,
		summaryJudgeSystemPrompt,
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
