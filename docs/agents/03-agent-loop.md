# L2 — Agent Loop Deep Dive

> Goal: Understand how the Agent executes multi-turn conversations.  
> Prerequisite: [01-overview.md](01-overview.md), [02-devkit.md](02-devkit.md)  
> Next: [04-tools.md](04-tools.md) for tool development.

---

## Loop Overview

The Agent loop (`agent/loop.go`) follows this cycle:

```
Run(tape, prompt)
    │
    ▼
┌─────────────┐
│ 1. Append   │  Write user prompt to tape
│    user msg │
└──────┬──────┘
       │
       ▼
┌─────────────┐     ┌─────────────┐
│ 2. Build    │◀────│ 7. Append   │
│    context  │     │    result   │  (loop back)
└──────┬──────┘     └─────────────┘
       │
       ▼
┌─────────────┐
│ 3. Call LLM │
│    (Chat)   │
└──────┬──────┘
       │
       ├── Text response ──▶ Done
       │
       └── Tool calls ──▶
              │
              ▼
       ┌─────────────┐
       │ 4. Execute  │
       │    tools    │
       └──────┬──────┘
              │
              ▼
       ┌─────────────┐
       │ 5. Append   │
       │    results  │
       └──────┬──────┘
              │
              ▼
       ┌─────────────┐
       │ 6. Check    │
       │    max steps│  Exceeded? ──▶ Error
       └─────────────┘
```

---

## Run Methods

### Run

```go
func (a *Agent) Run(ctx, tapeName, prompt string, historyAfterEntryID int32) (*Result, error)
```

- `tapeName`: Identifies the conversation tape. Each tape is isolated.
- `prompt`: User message content.
- `historyAfterEntryID`: Resume from a specific entry (0 = from start).
- Returns `*Result` with `.Output` (final text) and `.ToolCalls` history.

### RunWithOpts

```go
func (a *Agent) RunWithOpts(ctx, tapeName, prompt string, historyAfterEntryID int32, maxSteps int, contextJSON string) (*Result, error)
```

- `maxSteps`: Override `Config.MaxSteps` for this run (0 = use config).
- `contextJSON`: JSON-encoded map passed to tool handlers.

### RunWithOptsAndTools

```go
func (a *Agent) RunWithOptsAndTools(ctx, tapeName, prompt string, historyAfterEntryID int32, maxSteps int, allowedTools *[]string, contextJSON string) (*Result, error)
```

- `allowedTools`: Whitelist of visible tools. `nil` = all tools. Empty slice `[]string{}` = no tools (text-only).

---

## Step-by-Step Execution

### Step 1: Append User Message

The prompt is appended as a `user` Entry to the tape.

### Step 2: Build Context

1. Fetch tape entries from `historyAfterEntryID` to latest
2. Apply anchor: if an `anchor` Entry exists, context starts from the most recent anchor
3. Convert entries to LLM message format
4. Prepend system prompt (composed from base + hooks)

### Step 3: Call LLM

`ChatClient.Chat()` sends the message list to the LLM API.

If the model requests tools (`tool_calls`), the response contains:
- `Role: assistant`
- `ToolCalls: []ToolCall{...}`

The tool call request is appended to tape as an `assistant` Entry.

### Step 4: Execute Tools

`ToolExecutor.Execute()` runs each tool call:

1. Look up tool by name
2. Call `BeforeToolCall` hooks (policy checks)
3. Execute handler with `ToolContext`
4. Call `AfterToolCall` hooks
5. Handle errors and result formatting

### Step 5: Append Results

Each tool result is appended as a `tool` Entry.

### Step 6: Check Max Steps

If the total tool call iterations exceed `MaxSteps`, return error.

### Step 7: Loop

Go back to Step 2 with updated context (now including tool results).

---

## Run Modes

The `runMode` struct controls optional behavior per run:

```go
type runMode struct {
    tapeContextOverride *tape.TapeContext  // Override tape context
    maxSteps            int                // Step limit override
    excludeToolNames    map[string]struct{} // Tools to hide
    toolWhitelist       bool               // Enable whitelist
    allowedToolNames    map[string]struct{} // Whitelisted tools
}
```

---

## Per-Tape Features

### Per-Tape Model Selection

Configure different models per tape via glob patterns:

```go
cfg := agent.Config{
    TapeModels: map[string]string{
        "code_*": "gpt-4o",      // Coding tasks use strong model
        "chat_*": "gpt-4o-mini", // Chat use fast model
    },
}
```

### Per-Tape System Prompt

```go
cfg := agent.Config{
    SystemPromptBases: map[string]string{
        "code_*": "You are a senior Go engineer.",
        "doc_*":  "You are a technical writer.",
    },
}
```

### Model Switching at Runtime

```go
agent.SwitchModel(tapeName, "claude-sonnet-4-6")
```

---

## Subagent Delegation

Agents can delegate to subagents via the `subagent.go` mechanism:

```go
// In a tool handler, create a subagent run
subResult, err := agent.RunWithOpts(ctx, subTape, task, 0, maxSteps, contextJSON)
```

Key behaviors:
- Subagent uses its own tape (isolated history)
- Parent can limit subagent's visible tools
- Subagent results are summarized back to parent

---

## Reactive Handoff

`agent/reactive_handoff.go` implements context handoff between agents:

1. Source agent reaches a handoff trigger (e.g. "transfer to billing")
2. Source compacts its context into a handoff package
3. Target agent receives the package and resumes

Useful for:
- Department routing (sales → support → billing)
- Escalation paths
- Multi-agent systems

---

## Result Type

```go
type Result struct {
    Output    string      // Final text response
    ToolCalls []ToolCall  // History of tool invocations
}
```

When the loop ends with a text response (no more tool calls), `Output` contains the final message.

---

## Hooks Integration

The loop invokes hooks at key points:

| Hook Point | Method | Purpose |
|------------|--------|---------|
| Before run | `ComposeSystemPrompt` | Build final system prompt |
| Per turn | `GetTools` | Get available tools |
| Before tool | `BeforeToolCall` | Policy check |
| After run | `AfterAgentRun` | Cleanup, metrics, SSE push |

---

## Error Handling

| Error Type | Cause | Behavior |
|------------|-------|----------|
| Max steps exceeded | Loop > MaxSteps | Return error, no result |
| Tool not found | LLM hallucinated tool | Return error to LLM, may retry |
| Tool execution error | Handler panics/returns error | Error recorded in tape, LLM sees error message |
| LLM API error | Network/Rate limit | Propagated to caller |
