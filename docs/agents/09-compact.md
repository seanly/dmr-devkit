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

## Configuration

### Agent Config

```go
type AgentConfig struct {
    MaxContextTokens    int     // Hard limit for context window
    CompactThreshold    float64 // Trigger compaction at this ratio (0.8 = 80%)
    CompactPreserveLast int     // Keep N recent entries uncompressed
}
```

### Per-Tool Limits

```go
tool := &tool.Tool{
    Spec: tool.ToolSpec{
        Name:           "search_web",
        MaxResultChars: 8000,  // Externalize if result > 8K chars
    },
}
```

### Global Default

```go
// In agent.Config
defaultToolResultMaxChars = 8000  // Applied when MaxResultChars = 0
```

---

## Compaction Policy (`agent/compact.go`)

The compaction orchestrator decides WHEN and WHAT to compact:

```go
func (a *Agent) shouldCompact(tapeName string) bool {
    // Check token estimate against threshold
    // Consider last compact time
    // Respect minimum interval between compacts
}

func (a *Agent) compact(tapeName string) error {
    // 1. Determine compact range
    // 2. Call prompt compaction
    // 3. Write anchor
    // 4. Update last compact tracking
}
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

4. **Use tool groups** to reduce per-turn context

### For Operators

1. **Monitor token usage** via `AfterAgentRun` hook
2. **Set workspace cleanup** for externalized results
3. **Configure SQLite FTS5** for fast tape queries
