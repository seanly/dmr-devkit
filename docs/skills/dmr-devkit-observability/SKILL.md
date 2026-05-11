---
name: dmr-devkit-observability
description: >
  This skill should be used when the user wants to "persist conversations",
  "tape storage", "monitor agent", "observability", "event stream",
  "SQLite storage", "PostgreSQL tape", "debug production", or needs guidance
  on DMR devkit observability and storage backends.
  Part of the DMR devkit skills suite.
  Covers tape storage backends, event streams, and tracing considerations.
  Do NOT use for deployment setup (use dmr-devkit-a2a) or agent code patterns
  (use dmr-devkit-agent).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
---

# Devkit Observability Guide

> **Before using this skill**, activate `/dmr-devkit-workflow` first — it contains the required development phases.

## Observability Tiers

Choose the right level based on your needs:

| Tier | What It Does | Scope | Default | Best For |
|------|-------------|-------|---------|----------|
| **In-Memory Tape** | Conversation history in RAM | Single process | Yes (devkit default) | Prototyping, short-lived scripts |
| **File Tape** | Append-only JSONL files | Single node | No | Local development with persistence |
| **SQLite Tape** | Structured queryable storage | Single node | No | Lightweight production, embedded apps |
| **PostgreSQL / MySQL** | Distributed persistent storage | Multi-node | No | Production deployments, shared state |
| **Event Stream** | Real-time execution events | All runners | No | UI progress bars, debugging, logging |

---

## Storage Backend Configuration

### In-Memory (Default)

```go
// Default — no config needed
kit, err := devkit.Build(ctx, opts)
```

- Fastest, no disk I/O
- **Data lost on process exit**
- Good for: prototypes, CI tests, one-off scripts

### File Storage

```go
opts.TapeConfig = tape.StoreConfig{
    Driver: "file",
    Dir:    "./tapes",
}
```

- Append-only JSONL files in the specified directory
- One file per tape
- Good for: local development, simple persistence

### SQLite

```go
opts.TapeConfig = tape.StoreConfig{
    Driver: "sqlite",
    DSN:    "./tapes.db",
}
```

- Single-file database
- Queryable via SQL
- Good for: embedded deployments, lightweight production

### PostgreSQL

```go
opts.TapeConfig = tape.StoreConfig{
    Driver: "postgres",
    DSN:    "postgres://user:pass@localhost/dmr?sslmode=disable",
}
```

- Full-featured RDBMS
- Supports concurrent access
- Good for: multi-instance deployments, analytics

### MySQL

```go
opts.TapeConfig = tape.StoreConfig{
    Driver: "mysql",
    DSN:    "user:pass@tcp(localhost:3306)/dmr?parseTime=true",
}
```

---

## Tape Timezone

Set the timezone for tape timestamps:

```go
opts.TapeTimezone = "Asia/Shanghai"
```

---

## Inspecting Tape History

```go
// Fetch all entries for a tape
entries, err := kit.Store.FetchAll("my-tape", nil)
for _, entry := range entries {
    fmt.Printf("[%s] %s: %s\n", entry.Timestamp, entry.Type, entry.Content)
}
```

Common entry types:

| Type | Description |
|------|-------------|
| `message` | LLM or user message |
| `tool_call` | Tool invocation request |
| `tool_result` | Tool execution result |
| `anchor` | Context window anchor point |
| `event` | System event |

---

## Event Streams

All workflow runners support real-time event streaming:

```go
for ev, err := range kit.RunWorkflowStream(ctx, runner, input) {
    if err != nil {
        log.Printf("Error: %v", err)
        continue
    }
    // Log to file, send to WebSocket, update UI, etc.
    logEvent(ev)
}
```

Event types:

| Type | When |
|------|------|
| `workflow_start` | Workflow begins |
| `workflow_end` | Workflow completes |
| `node_start` | Node begins |
| `node_end` | Node completes |
| `node_skip` | Node skipped (resume) |
| `interrupt` | Human-in-the-loop pause |

---

## Context Window & Compression

DMR agents automatically manage context window size:

- **Anchor mechanism**: `anchor` entries mark restart points for context reading
- **Compact/Handoff**: When context exceeds thresholds, older history is compressed into a summary
- **Per-tape isolation**: Each tape maintains its own context window

You can configure thresholds via `AgentPolicy` in `devkit.Options`:

```go
opts.AgentPolicy = config.AgentConfig{
    // Context delivery and token limit policies
    MaxContextEntries: 100,
    // ... other policies
}
```

---

## Production Observability Checklist

- [ ] Use persistent storage (SQLite/PostgreSQL) instead of in-memory
- [ ] Set `TapeTimezone` for consistent timestamps
- [ ] Implement event stream consumers for logging/alerting
- [ ] Monitor tape growth — old tapes may need archiving
- [ ] Back up tape databases regularly

---

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| "database is locked" (SQLite) | Concurrent writes | Use PostgreSQL for concurrent access, or serialize access |
| Tape entries missing | In-memory store + process restart | Switch to file/SQLite/PostgreSQL |
| Timezone wrong | `TapeTimezone` not set | Set `opts.TapeTimezone` before `devkit.Build` |
| Event stream not yielding | Not using `RunWorkflowStream` | Use it instead of `RunWorkflow` |
| Storage directory not created | Missing `Dir` path | Ensure the directory exists or is creatable |

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-agent` — Agent API quick reference
- `/dmr-devkit-a2a` — A2A service exposure
- `/dmr-devkit-orchestration` — Workflow event streams
