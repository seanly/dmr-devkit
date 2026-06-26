package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/tape"
)

// JudgeFunc scores a tape slice for an LLM rubric dimension.
// It returns the raw score, a human-readable detail, and per-call metadata.
type JudgeFunc func(ctx context.Context, entries []tape.TapeEntry, spec JudgeSpec) (score float64, detail string, meta JudgeMeta, err error)

// ChatFunc performs a single-turn LLM call (used by CLI wiring).
// It returns the model's text and metadata about the call.
type ChatFunc func(ctx context.Context, prompt string) (string, JudgeMeta, error)

// LLMJudge returns a JudgeFunc backed by chat.
func LLMJudge(chat ChatFunc) JudgeFunc {
	return func(ctx context.Context, entries []tape.TapeEntry, spec JudgeSpec) (float64, string, JudgeMeta, error) {
		var meta JudgeMeta
		if chat == nil {
			return 0, "", meta, fmt.Errorf("nil chat func")
		}
		prompt := strings.TrimSpace(spec.Prompt)
		if prompt == "" {
			prompt = defaultJudgePrompt()
		}
		full := prompt + "\n\n--- TAPE ---\n" + FormatTapeSummary(entries) + "\n--- END TAPE ---\n\nReply with JSON: {\"score\": <number>, \"reason\": \"...\"}"
		start := time.Now()
		text, chatMeta, err := chat(ctx, full)
		meta.Latency = time.Since(start)
		meta.Tokens = chatMeta.Tokens
		meta.Cost = chatMeta.Cost
		meta.Model = chatMeta.Model
		if err != nil {
			return 0, "", meta, err
		}
		score, reason := parseJudgeResponse(text)
		if reason == "" {
			reason = strings.TrimSpace(text)
		}
		return score, reason, meta, nil
	}
}

func defaultJudgePrompt() string {
	return "Score how well the agent tape satisfies the rubric dimension. Use score 0-10 where 10 is fully satisfied."
}

// CachingJudge wraps a JudgeFunc with an in-memory cache keyed by tape hash + spec.
func CachingJudge(inner JudgeFunc) JudgeFunc {
	cache := &judgeCache{
		data: map[string]judgeCacheEntry{},
	}
	return func(ctx context.Context, entries []tape.TapeEntry, spec JudgeSpec) (float64, string, JudgeMeta, error) {
		key := judgeCacheKey(entries, spec)
		cache.mu.Lock()
		if e, ok := cache.data[key]; ok {
			cache.mu.Unlock()
			meta := e.meta
			meta.CacheHit = true
			return e.score, e.detail, meta, nil
		}
		cache.mu.Unlock()
		score, detail, meta, err := inner(ctx, entries, spec)
		if err != nil {
			return score, detail, meta, err
		}
		cache.mu.Lock()
		cache.data[key] = judgeCacheEntry{score: score, detail: detail, meta: meta}
		cache.mu.Unlock()
		return score, detail, meta, nil
	}
}

type judgeCacheEntry struct {
	score  float64
	detail string
	meta   JudgeMeta
}

type judgeCache struct {
	mu   sync.Mutex
	data map[string]judgeCacheEntry
}

func judgeCacheKey(entries []tape.TapeEntry, spec JudgeSpec) string {
	h := sha256.New()
	data, _ := json.Marshal(entries)
	h.Write(data)
	data, _ = json.Marshal(spec)
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// EnsembleJudge averages scores across multiple judges.
func EnsembleJudge(judges []JudgeFunc) JudgeFunc {
	return func(ctx context.Context, entries []tape.TapeEntry, spec JudgeSpec) (float64, string, JudgeMeta, error) {
		if len(judges) == 0 {
			return 0, "", JudgeMeta{}, fmt.Errorf("no judges in ensemble")
		}
		type result struct {
			score  float64
			detail string
			meta   JudgeMeta
			err    error
		}
		results := make([]result, len(judges))
		var wg sync.WaitGroup
		for i, j := range judges {
			wg.Add(1)
			go func(idx int, judge JudgeFunc) {
				defer wg.Done()
				s, d, m, err := judge(ctx, entries, spec)
				results[idx] = result{score: s, detail: d, meta: m, err: err}
			}(i, j)
		}
		wg.Wait()

		var sum float64
		var tokens int
		var latency time.Duration
		var details []string
		for i, r := range results {
			if r.err != nil {
				return 0, "", JudgeMeta{}, fmt.Errorf("judge %d failed: %w", i, r.err)
			}
			sum += r.score
			tokens += r.meta.Tokens
			latency += r.meta.Latency
			details = append(details, fmt.Sprintf("judge%d=%.2f", i, r.score))
		}
		avg := sum / float64(len(judges))
		meta := JudgeMeta{Tokens: tokens, Latency: latency}
		return avg, "ensemble: " + strings.Join(details, ", "), meta, nil
	}
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
