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

# Progressive context management (7-layer alignment)
graduated_trim = true
recent_tool_results = 3
old_tool_result_ratio = 0.25
snip_enabled = true
microcompact = true
max_compacts_per_anchor = 2
session_memory = true
session_memory_fallback = true
```

- `compact_strategy`: `summary` (default), `snip`, `collapse`, `hybrid`.
- `graduated_trim` / `recent_tool_results` / `old_tool_result_ratio`: L1 — older `tool_result` content is truncated to `tool_result_max_chars * ratio` while the most recent N stay intact.
- `snip_enabled`: L2 — drop oldest non-system, non-summary messages when approaching the threshold, deferring an LLM compact.
- `microcompact`: L3 — clear older `tool_result` content into placeholders on read (read-time only; the tape store is untouched).
- `max_compacts_per_anchor`: cap voluntary (preemptive/proactive) compacts per conversation cycle; once reached the loop forces lighter degradation instead of "summary of a summary".
- `session_memory` / `session_memory_fallback`: L5/L6 — a locally-assembled, incrementally-maintained conversation memory serves as the compact summary directly, avoiding an LLM call. When too sparse, the loop falls back to LLM summarization (if `session_memory_fallback = true`).

> Note: the legacy `rolling_summary`, `rolling_summary_full_refresh`, and `semantic_collapse`
> config keys have been removed. Rolling-summary inheritance is now served from a per-tape
> cache populated when a `compact_summary` is written (`findLatestCompactSummary` scans
> newest-first with early exit). `SemanticCollapseMessages` and its helpers
> (`semanticCollapseMessages`, `extractToolCalls`, `toolCallNameAndArgs`) were removed entirely
> — they had no production callers; session memory records tool-interaction semantics instead.
> The LLM summary judge (`compact_judge.go`) was also removed entirely.

### Agent Config

```toml
[agent]
max_token = 128000
handoff_threshold = 0.75
```

---

## Progressive Context Management (7-layer alignment)

dmr-devkit aligns with Claude Code's layered context model. Each layer is
"lighter before heavier" so an LLM compact is the last resort, not the first.

| Layer | Mechanism | Config | File |
|-------|-----------|--------|------|
| L1 | Graduated tool_result trimming | `graduated_trim` | `tape/builder.go` |
| L2 | History snip (drop oldest safe units) | `snip_enabled` | `agent/snip.go`, `tape/builder.go` |
| L3 | Microcompact (clear old tool content) | `microcompact` | `tape/builder.go` |
| L4 | Compact coordinator (cooldown + cap) | `max_compacts_per_anchor` | `agent/compact_coordinator.go` |
| L5 | Session memory summary (no LLM) | `session_memory` | `agent/session_memory.go` |
| L6 | Post-compact context rebuild | — (automatic) | `agent/post_compact_rebuild.go` |
| L7 | Reactive recovery (lightweight retry → compact) | `snip_enabled` | `agent/reactive_recovery.go` |

### Compact Coordinator (L4)

All compact triggers (preemptive, proactive, reactive, manual) route through a
single `CompactCoordinator` per tape. It enforces:

- A cooldown gap (`compact_gap`) between voluntary compacts.
- A pressure override (`pressure_override_gap`) when tokens already exceed the threshold.
- A per-cycle cap (`max_compacts_per_anchor`) that forces lighter degradation once reached.

Reactive (overflow) and manual compacts bypass the cap; the loop's
`autoHandoffDone` guard and `reactiveSnipAttempts` bound retries.

### Session Memory (L5)

`SessionMemory` captures lightweight, rule-extracted segments each turn
(user intent, tool results, file changes, errors, decisions) — no LLM calls.
On compact, the memory is assembled into a structured 9-section summary and
written directly, skipping the summarizer LLM call entirely in the common case.

### Post-compact rebuild (L6)

After a compact, `rebuildPostCompactContext` appends a system entry listing
recently-accessed files (extracted from `tool_call` entries) and the names of
already-discovered tools, so the model does not start the post-compact turn
from an empty context.

### Reactive recovery (L7)

On a context-overflow API error, the loop first tries a lightweight recovery
(aggressive snip + microcompact on the next context build) and retries the LLM
call. If it overflows again, it escalates to a full compact. This avoids an
expensive compact when a quick trim would suffice.

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

4. **Enable session memory** (`session_memory = true`) for conversations with
   frequent compacts — most compacts then need no LLM call at all.

5. **Enable graduated trim + microcompact** for tool-heavy sessions to shed
   older tool_result tokens without compaction.

### For Operators

1. **Monitor token usage** via `AfterAgentRun` hook
2. **Set workspace cleanup** for externalized results
3. **Configure SQLite FTS5** for fast tape queries

---

## Migrating from earlier configs

- `rolling_summary` / `rolling_summary_full_refresh` config keys are removed. Rolling-summary inheritance is now a per-tape cache populated on compact; the summarizer reuses it via `optimizeEntriesForSummaryWithCache`.
- `semantic_collapse` config key is removed; `SemanticCollapseMessages` and its helpers are deleted (no production callers remained).
- The LLM summary judge (`agent/compact_judge.go`) is removed; the heuristic `evaluateCompactSummary` remains as the quality guard.
- `quality_fallback = true` skips writing poor summaries to tape entirely (previously it only suppressed them at read time).
- New progressive-management keys (`graduated_trim`, `snip_enabled`, `microcompact`, `max_compacts_per_anchor`, `session_memory`, `session_memory_fallback`) are all opt-in (default false).
- New metrics appear automatically in `loop:compact` events (`strategy = "session_memory"` marks a no-LLM compact).
