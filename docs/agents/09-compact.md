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

### 2. Preemptive Compaction (`agent/loop.go`)

Proactively compacts before the threshold is reached, based on token estimation.

```toml
[agent]
max_token = 128000
handoff_threshold = 0.75
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

## Rolling Summary

Rolling summary is an **incremental** compaction mode. Instead of re-summarizing every message since the last anchor on each compact, it summarizes only the messages added since the previous `compact_summary`.

### Why It Helps

- Reduces redundant summarizer work in long conversations.
- Keeps summarizer input size bounded.
- Matches Claude Code's layered context model: older history lives in the previous summary, and only newer full-fidelity messages need fresh summarization.

### How It Works

```
anchor ──→ compact_summary_v1 (full window)
            │
            ▼
     new messages ──→ merge with v1 ──→ compact_summary_v2
            │
            ▼
     new messages ──→ merge with v2 ──→ compact_summary_v3
```

1. Read entries since the last anchor.
2. Find the latest `compact_summary`.
3. Use its content as `[Previous Context Summary]`.
4. Slice entries to keep only messages **after** that summary.
5. Send the previous summary + new messages to the LLM.
6. Write the new `compact_summary`.

### Configuration

```toml
[agent.context]
rolling_summary = true
rolling_summary_full_refresh = 5
```

- `rolling_summary`: enable incremental compaction.
- `rolling_summary_full_refresh`: every N rolling compacts, force a full-window compact to prevent drift. `0` disables full refresh.

### Full Refresh

After many rolling compacts, older details can drift. A periodic full refresh re-summarizes the whole anchor-to-end window to produce a fresh baseline.

The agent counts `compact_summary` entries since the last anchor. When the count reaches `rolling_summary_full_refresh`, the next compact uses the full window.

### Edge Cases

- **First compact**: no previous summary exists, so the full window is summarized.
- **Quality fallback**: a poor summary is skipped. The next rolling compact uses the most recent *good* summary as its boundary.
- **Empty window after boundary**: nothing new to summarize; the LLM call is skipped.
- **Multiple compact summaries between anchors**: always use the latest as the boundary.

---

## Semantic Collapse

Semantic collapse restructures tool interactions for the summarizer. An `assistant` message with `tool_calls` followed by matching `tool` result messages is folded into a single compact `user` message.

### Why It Helps

- Tool interactions create multiple messages per turn.
- Collapsing them reduces token volume while preserving the semantic content.
- Inspired by Claude Code's `contextCollapse` layer, but applied only to summarizer input so API compatibility is preserved.

### Collapsed Format

```text
[Tool Interaction]
Tool: read_file (call_id: call_001)
Arguments: {"file_path": "README.md"}
Result: # README
...

---

Tool: search (call_id: call_002)
Arguments: {"query": "go context"}
Result: found 3 matches
```

### Configuration

```toml
[agent.context]
compact_strategy = "semantic_collapse"
```

or as an optimization pass alongside another strategy:

```toml
[agent.context]
compact_strategy = "hybrid"
semantic_collapse = true
```

- `semantic_collapse`: enable the tool-interaction collapse pass.
- For live context, `semantic_collapse` is treated as `summary` (identity transform) because OpenAI-compatible APIs require separate `assistant`/`tool` roles.

### Algorithm

1. Scan messages left-to-right.
2. When an `assistant` message with `tool_calls` is found, collect immediately following `tool` messages whose `tool_call_id` matches a call ID.
3. Replace the group with a single `user` message containing `[Tool Interaction]` blocks.
4. Preserve unmatched messages unchanged.

### Edge Cases

- **Multiple results per assistant call**: group by `tool_call_id`.
- **Missing `tool_call_id`**: fallback to positional pairing.
- **Parallel tool calls**: assistant may issue several calls; collect all matching results.
- **Very large results**: apply existing `maxToolContentLength` truncation.
- **Tool result without preceding assistant tool_calls**: leave unchanged.

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

Every compaction writes a `loop:compact` event to the tape:

```json
{
  "success": true,
  "summary_chars": 1200,
  "quality": "good",
  "judge_pass": true,
  "trigger_reason": "proactive",
  "original_tokens": 15000,
  "optimized_tokens": 12000,
  "strategy": "summary",
  "rolling_summary": true,
  "rolling_full_refresh": false,
  "rolling_count": 2,
  "summarized_since_id": 42,
  "previous_summary_chars": 800,
  "semantic_collapse": false
}
```

- `trigger_reason`: `preemptive`, `proactive`, `overflow`, `manual`, or `tool`.
- `original_tokens`: estimated tokens before summarizer optimization.
- `optimized_tokens`: estimated tokens sent to the summarizer.
- `strategy`: configured compact strategy.
- `rolling_summary` / `rolling_full_refresh` / `rolling_count`: rolling summary state.
- `semantic_collapse`: whether semantic collapse was applied.

---

## Quality Fallback

When LLM summarization produces a poor-quality summary, `quality_fallback = true` changes behavior:

1. The bad summary is **not written** to the tape.
2. Only an anchor + event are written.
3. The anchor carries a hint to expand the soft boundary so more raw messages are retained on the next turn.

```toml
[agent.context]
quality_fallback = true
quality_fallback_keep_before = 8
```

If `quality_fallback_keep_before` is `0`, the agent doubles `keep_before_anchor` (floor 6, cap 12).

---

## Compact Cadence

Automatic compacts are rate-limited:

```toml
[agent.context]
compact_gap = 3
pressure_override_gap = 1
```

- `compact_gap`: minimum steps between automatic compacts.
- `pressure_override_gap`: allow early compact when tokens already exceed the threshold.

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

rolling_summary = false
rolling_summary_full_refresh = 5

semantic_collapse = false
```

- `compact_strategy`: `summary` (default), `snip`, `collapse`, `hybrid`, `semantic_collapse`.
- `rolling_summary` / `rolling_summary_full_refresh`: rolling summary control.
- `semantic_collapse`: enable tool-interaction collapse for summarizer input.

### Agent Config

```toml
[agent]
max_token = 128000
handoff_threshold = 0.75
```

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

4. **Use rolling summary** for conversations with frequent compacts

5. **Use semantic collapse** for tool-heavy sessions

### For Operators

1. **Monitor token usage** via `AfterAgentRun` hook
2. **Set workspace cleanup** for externalized results
3. **Configure SQLite FTS5** for fast tape queries

---

## Migrating from earlier configs

- `rolling_summary` was removed in an earlier version because it was unimplemented; it is now a real feature.
- `quality_fallback = true` now skips writing poor summaries to tape entirely (previously it only suppressed them at read time).
- New metrics appear automatically in `loop:compact` events.
