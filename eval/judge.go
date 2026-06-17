package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/seanly/dmr-devkit/tape"
)

// JudgeFunc scores a tape slice for an LLM rubric dimension.
type JudgeFunc func(ctx context.Context, entries []tape.TapeEntry, spec JudgeSpec) (score float64, detail string, err error)

// ChatFunc performs a single-turn LLM call (used by CLI wiring).
type ChatFunc func(ctx context.Context, prompt string) (string, error)

// LLMJudge returns a JudgeFunc backed by chat.
func LLMJudge(chat ChatFunc) JudgeFunc {
	return func(ctx context.Context, entries []tape.TapeEntry, spec JudgeSpec) (float64, string, error) {
		if chat == nil {
			return 0, "", fmt.Errorf("nil chat func")
		}
		prompt := strings.TrimSpace(spec.Prompt)
		if prompt == "" {
			prompt = defaultJudgePrompt()
		}
		full := prompt + "\n\n--- TAPE ---\n" + FormatTapeSummary(entries) + "\n--- END TAPE ---\n\nReply with JSON: {\"score\": <number>, \"reason\": \"...\"}"
		text, err := chat(ctx, full)
		if err != nil {
			return 0, "", err
		}
		score, reason := parseJudgeResponse(text)
		if reason == "" {
			reason = strings.TrimSpace(text)
		}
		return score, reason, nil
	}
}

func defaultJudgePrompt() string {
	return "Score how well the agent tape satisfies the rubric dimension. Use score 0-10 where 10 is fully satisfied."
}

// FormatTapeSummary renders tape entries for LLM judge input.
func FormatTapeSummary(entries []tape.TapeEntry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "[%s id=%d]\n", e.Kind, e.ID)
		switch e.Kind {
		case "message":
			role, _ := e.Payload["role"].(string)
			content, _ := e.Payload["content"].(string)
			fmt.Fprintf(&b, "%s: %s\n", role, truncateSummary(content, 2000))
		case "tool_call":
			if calls, ok := tape.ExtractToolCalls(e.Payload); ok {
				for _, c := range calls {
					fmt.Fprintf(&b, "tool_call: %s(%s)\n", c.Name, truncateSummary(c.Arguments, 500))
				}
			}
		case "tool_result":
			fmt.Fprintf(&b, "tool_result: %s\n", truncateSummary(fmt.Sprint(e.Payload["results"]), 1000))
		case "task_state", "handoff_packet", "event", "anchor", "compact_summary":
			fmt.Fprintf(&b, "%s\n", truncateSummary(fmt.Sprint(e.Payload), 1500))
		default:
			fmt.Fprintf(&b, "%s\n", truncateSummary(fmt.Sprint(e.Payload), 500))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func parseJudgeResponse(text string) (score float64, reason string) {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err == nil {
		if v, ok := m["score"]; ok {
			score = anyToFloat(v)
		}
		if r, ok := m["reason"].(string); ok {
			reason = strings.TrimSpace(r)
		}
		return score, reason
	}
	for _, tok := range strings.Fields(text) {
		if v, err := strconv.ParseFloat(strings.Trim(tok, ",."), 64); err == nil {
			return v, text
		}
	}
	return 0, text
}

func anyToFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		return 0
	}
}

func truncateSummary(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
