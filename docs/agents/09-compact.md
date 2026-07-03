# L3 — Context Compaction and Optimization

> Goal: Understand how dmr-devkit manages long conversations.  
> Prerequisite: [01-overview.md](01-overview.md), [03-agent-loop.md](03-agent-loop.md), [06-tape.md](06-tape.md)

---

## Problem

As conversations grow, the context window fills with:
- Long message histories
- Large tool results (file contents, search results, API responses)
- Redundant back-and-forth

This causes:
- Higher token costs
- Slower LLM responses
- Potential context window overflow

---

## Compaction Strategies

### 1. Prompt Compaction (`agent/compact_prompt.go`)

Uses the LLM itself to summarize conversation history into a condensed system prompt fragment.

**Trigger**: Context exceeds configured threshold

**Process**:
1. Select entries from start to compact point
2. Send to LLM with compaction prompt: "Summarize this conversation..."
3. Receive condensed summary
4. Write `anchor` Entry with summary
5. Subsequent context builds from anchor

```
Before:                    After:
[0] sys                    [0] sys
[1] user: hi               [1] anchor: "Summary: user greeted"
[2] asst: hello            [2] user: "Tell me more"
[3] user: tell me more     (context starts from [1])
[4] asst: ...
```

### 2. Preemptive Compaction (`agent/preemptive_compact.go`)

Proactively compacts before the threshold is reached, based on token estimation.

```go
// Configured via AgentConfig
preemptiveThreshold := 0.8  // Compact at 80% of max context
```

### 3. Micro-Compaction (`agent/toolresult/`)

Externalizes large tool results to disk:

```
Before:
[tool] read_file: "{100KB of file content}"

After:
[tool] read_file: "Result externalized to .dmr/toolresult/read_file_001.md (100KB)"
```

**Trigger**: `MaxResultChars` exceeded

**Process**:
1. Tool result exceeds limit
2. Write full result to `.dmr/toolresult/<toolname>_<seq>.md`
3. Replace tape entry with reference
4. LLM sees preview + file path

### 4. Reactive Handoff (`agent/reactive_handoff.go`)

Transports context between agents:

1. Source agent compacts its tape into a handoff package
2. Package contains: summary, key facts, pending tasks
3. Target agent receives package and continues

Useful for:
- Multi-department routing
- Agent specialization handoffs
- Context migration

---

## Token Estimation

`agent/token_estimator.go` estimates token count without calling the tokenizer:

```go
func EstimateTokens(text string) int {
    // Rough heuristic: ~4 chars per token for English
    // + overhead for message structure
    return len(text)/4 + messageOverhead
}
```

Used to:
- Decide when to trigger preemptive compaction
- Estimate cost before LLM call
- Warn about approaching limits

---

## Compaction Metrics

Every compaction writes a `loop:compact` event to the tape with the following fields:

```json
{
  "success": true,
  "summary_chars": 1200,
  "quality": "good",
  "judge_pass": true,
  "trigger_reason": "proactive",
  "original_tokens": 15000,
  "optimized_tokens": 12000,
  "strategy": "summary"
}
```

- `trigger_reason`: `preemptive`, `proactive`, `overflow`, `manual`, or `tool`.
- `original_tokens`: estimated tokens before summarizer optimization.
- `optimized_tokens`: estimated tokens actually sent to the summarizer.
- `strategy`: the configured `compact_strategy`.

These metrics are also emitted as a span event when an OpenTelemetry-aligned tracer is configured.

---

## Quality Fallback

When LLM summarization produces a poor-quality summary, `quality_fallback = true` changes behavior:

1. The bad summary is **not written** to the tape.
2. Only an anchor + event are written.
3. The anchor carries a hint to expand the soft boundary so more raw messages are retained on the next turn.

This keeps `task_state` + recent raw messages available instead of injecting a misleading summary.

```toml
[agent.context]
quality_fallback = true
quality_fallback_keep_before = 8   # explicit raw-message window for poor compacts
```

If `quality_fallback_keep_before` is `0`, the agent doubles `keep_before_anchor` (with a floor of 6 and a cap of 12).

---

## Compact Cadence

Automatic compacts are rate-limited to avoid burning tokens on every step:

```toml
[agent.context]
compact_gap = 3               # minimum steps between automatic compacts
pressure_override_gap = 1     # allow early compact when already above threshold
```

Defaults match the previous hardcoded behavior. Lower values make compaction more aggressive; higher values let the context grow longer before summarizing.

---

## Configuration

### Context Config

```toml
[agent.context]
persist_system_prompt = false
soft_boundary = true
keep_before_anchor = 3
compact_strategy = "summary"

compact_gap = 3
pressure_override_gap = 1

quality_fallback = false
quality_fallback_keep_before = 0
```

- `compact_strategy`: `summary` (default), `snip`, `collapse`, `hybrid`.
- `compact_gap` / `pressure_override_gap`: control automatic compact cadence.
- `quality_fallback` / `quality_fallback_keep_before`: control poor-summary fallback.

### Agent Config

```toml
[agent]
max_token = 128000
handoff_threshold = 0.75
```

`max_token` is the context budget; `handoff_threshold` is the ratio at which preemptive/proactive compacts trigger.

---

## Optimization Tips

### For Developers

1. **Set appropriate MaxResultChars** for each tool
   - File readers: 4000-8000
   - Search tools: 4000
   - API callers: 2000-4000
   - Small tools (-1): never externalize

2. **Use TapeModels** to switch to cheaper models for simple tasks

3. **Enable preemptive compaction** for long-running sessions

4. **Use tool groups** to reduce per-turn context

### For Operators

1. **Monitor token usage** via `AfterAgentRun` hook
2. **Set workspace cleanup** for externalized results
3. **Configure SQLite FTS5** for fast tape queries

---

## Migrating from earlier configs

- `rolling_summary` was removed; it was never implemented and had no runtime effect.
- `quality_fallback = true` now skips writing poor summaries to tape entirely (previously it only suppressed them at read time).
- New metrics appear automatically in `loop:compact` events; no config change required.
- `compact_gap` and `pressure_override_gap` default to the previous hardcoded values, so omitting them preserves old behavior.
