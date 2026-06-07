# L1 — Core Overview

> Goal: Understand dmr-devkit in 5 minutes.  
> Next: [02-devkit.md](02-devkit.md) (start coding)

---

## What is dmr-devkit

`dmr-devkit` is an **embeddable Go LLM Agent runtime library**. It provides all core components needed to assemble a fully-featured Agent **without** config files, CLI, or plugin ecosystems.

### Typical Use Cases

| Scenario | Why devkit |
|----------|------------|
| Embed AI assistant in existing Go service | Pure library calls, no external deps |
| Rapid Agent prototype validation | Working in 20 lines of code |
| Tests needing full Agent loop | In-memory tape, no state |
| Expose Agent as HTTP service | `a2aserver` package registers routes directly |
| Orchestrate multi-step AI pipelines | `workflow` package: Sequential / Parallel / Graph |

### Relationship with dmr CLI

```
┌────────────────────────────────────────┐
│              dmr CLI                   │  ← Production: config, plugins, Web, Cron
│  ┌────────┐  ┌────────┐  ┌─────────┐ │
│  │ config │  │ plugin │  │webserver│ │
│  │  .toml │  │Manager │  │  /cron  │ │
│  └────┬───┘  └────┬───┘  └────┬────┘ │
│       └───────────┴───────────┘      │
│                   │                    │
│              go get dmr-devkit         │
│                   │                    │
│       ┌───────────┴───────────┐      │
│       │      dmr-devkit       │      │  ← This repo: core runtime
│       │  agent/client/tape/…  │      │
│       └───────────────────────┘      │
└────────────────────────────────────────┘
```

**Key principle**: devkit does not import dmr; dmr imports devkit. devkit accepts extensions from dmr through the `agent.Hooks` interface.

---

## Core Architecture

### Four Components

#### 1. Agent — Multi-turn Conversation Loop

Location: `agent/` package

Core responsibilities:
- Execute the **LLM call → tool execution → result feedback** auto loop
- Default max 20 steps (configurable via `MaxSteps`)
- Support **tool discovery**: non-core tools loaded on-demand, reducing per-turn context
- Support **tool whitelist**: restrict visible tools per run
- Auto **context compaction**: summarize history when exceeding thresholds

Entry method:
```go
agent.Run(ctx, tapeName, prompt, historyAfterEntryID)
```

#### 2. ChatClient — LLM Communication Layer

Location: `client/` package

Core responsibilities:
- Encapsulate communication with OpenAI-compatible APIs
- Support streaming (SSE) and non-streaming output
- Auto-handle tool call requests (`tool_calls`)
- Read historical context from Tape to build message lists

Underlying protocol conversion implemented by `provider/openai/`.

#### 3. TapeManager — Conversation Audit Trail

Location: `tape/` package

Core responsibilities:
- Persist conversation history ("tape")
- Each tape is a sequence of Entry records
- Entry types: `message`, `tool_call`, `tool_result`, `anchor`, `event`
- **Anchor mechanism**: marks context window start, supports reading from latest anchor
- Multiple backends: Memory (testing), File (local), SQLite (single-node), PostgreSQL (multi-node)

```go
type Entry struct {
    ID        int32
    Role      string      // user / assistant / tool / system
    Content   string
    ToolCalls []ToolCall  // when Role=assistant and requesting tools
    // ... other fields
}
```

#### 4. Hooks — Extension Point

Location: `agent/hooks.go`

```go
type Hooks interface {
    ComposeSystemPrompt(ctx, basePrompt, tapeName) (string, error)
    GetTools(tapeName) []*Tool
    BeforeToolCall(ctx, args) error
    AfterAgentRun(ctx, args) error
    // ...
}
```

dmr's `plugin.Manager` implements this interface, giving devkit the same plugin capabilities as the CLI.

---

## Data Flow

A typical Agent run:

```
User Input
    │
    ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│  Agent.Run  │───▶│ Tape.Append │    │             │
│             │    │ (user msg)  │    │             │
└──────┬──────┘    └─────────────┘    │             │
       │                               │             │
       ▼                               │             │
┌─────────────┐    ┌─────────────┐    │   Tape      │
│  ChatClient │◀───│ Tape.Fetch  │◀───┤  (history)  │
│  .Chat()    │    │(ctx window) │    │             │
└──────┬──────┘    └─────────────┘    │             │
       │                               │             │
       ▼                               │             │
┌─────────────┐                       │             │
│  LLM API    │                       │             │
│  Response   │                       │             │
└──────┬──────┘                       │             │
       │                              │             │
       ├── Plain text ──▶ Return to user            │
       │                              │             │
       └── Tool calls ──▶             │             │
              │                       │             │
              ▼                       │             │
       ┌─────────────┐   ┌────────┐  │             │
       │ToolExecutor │──▶│ Execute│  │             │
       │             │   │ Tool   │  │             │
       └──────┬──────┘   └────┬───┘  │             │
              │               │      │             │
              ▼               ▼      │             │
       ┌─────────────────────────┐   │             │
       │ Tape.Append(tool_result)│──▶│             │
       └─────────────────────────┘   └─────────────┘
              │
              └── Loop back to ChatClient.Chat()
```

---

## Key Abstractions

### Tape

Tape is the core abstraction of dmr-devkit. It is not a simple message list but an **audit trail**:

- Each conversation/task corresponds to one tape (identified by `tapeName`)
- Supports resuming conversation from any Entry ID (`historyAfterEntryID`)
- Uses `anchor` Entry to mark context window, enabling resumption after compaction
- Supports export (`tape/export.go`) to JSON/NDJSON

### Tool Groups

| Group | Load Timing | Purpose |
|-------|-------------|---------|
| `core` | Loaded every LLM turn | High-frequency tools, always available |
| `extended` | Loaded after `toolSearch` discovery | Low-frequency tools, reduce context |
| `mcp` | Same as extended | Tools via MCP protocol |

### Context Compaction

When tape history grows too long, Agent auto-triggers compaction:

1. **Prompt compaction**: Use LLM to summarize history into system prompt fragments
2. **Micro-compaction**: Externalize large tool results to disk; tape only keeps references
3. **Anchor reset**: Write `anchor` Entry after compaction; subsequent context starts here

---

## Development Entry Point

### Minimal Code

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/seanly/dmr-devkit/devkit"
)

func main() {
    ctx := context.Background()
    kit, err := devkit.Build(ctx, devkit.Options{
        Model:  os.Getenv("AI_MODEL"),
        APIKey: os.Getenv("AI_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }
    defer kit.Close(ctx)

    res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, "Hello", 0)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Output)
}
```

### What's Next

- **To add tools** → Read [04-tools.md](04-tools.md)
- **To orchestrate multi-step tasks** → Read [05-workflow.md](05-workflow.md)
- **To persist storage** → Read [06-tape.md](06-tape.md)
- **To expose HTTP service** → Read [07-a2a.md](07-a2a.md)
