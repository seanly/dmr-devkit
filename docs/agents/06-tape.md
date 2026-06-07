# L2 — Tape Storage System

> Goal: Understand how conversation history is stored and managed.  
> Prerequisite: [01-overview.md](01-overview.md)  
> Related: [03-agent-loop.md](03-agent-loop.md) for how tape is used in the loop.

---

## What is Tape

Tape is an **audit trail** of conversation entries. Unlike a simple message array, it records the full lifecycle of an Agent interaction:

- User messages
- Assistant responses (including tool call requests)
- Tool execution results
- Anchor points (context window markers)
- Events (compact, handoff, etc.)

---

## Entry Types

```go
type Entry struct {
    ID        int32
    Role      EntryRole    // user / assistant / tool / system / anchor / event
    Content   string
    ToolCalls []ToolCall   // For assistant entries requesting tools
    Name      string       // Tool name (for tool role)
    Anchor    bool         // True for anchor entries
    Event     string       // Event type (for event role)
    Meta      map[string]any  // Additional metadata
}
```

### Role Semantics

| Role | When Created | Content |
|------|-------------|---------|
| `user` | Agent.Run called | User prompt |
| `assistant` | LLM responds | Text response OR tool_calls JSON |
| `tool` | Tool executed | Tool result |
| `system` | Agent initialized | System prompt (usually first entry) |
| `anchor` | Compaction triggered | Summary text |
| `event` | Special events | Event payload |

---

## TapeManager

```go
type TapeManager struct {
    store TapeStore
    // ...
}

func (tm *TapeManager) Append(tapeName string, entries ...*Entry) error
func (tm *TapeManager) FetchAll(tapeName string, filter *QueryFilter) ([]*Entry, error)
func (tm *TapeManager) FetchAfter(tapeName string, afterID int32) ([]*Entry, error)
```

### Context Window Building

When the Agent loop needs context:

1. `FetchAll(tapeName, nil)` retrieves all entries
2. Find the most recent `anchor` entry
3. Use entries from anchor onwards as context window
4. Prepend system prompt

---

## Storage Backends

### Memory Store

```go
// Default when no store config provided
opts := devkit.Options{Model: "gpt-4o", APIKey: key}
kit, _ := devkit.Build(ctx, opts)  // Uses memory store
```

- Fast, no persistence
- Lost on process exit
- Best for: testing, prototypes

### File Store

```go
opts.TapeConfig = tape.StoreConfig{
    Driver:    "file",
    Workspace: "./workspace",
}
```

- One file per tape (`<workspace>/.dmr/tapes/<tape>.jsonl`)
- NDJSON format, append-only
- Best for: local development, single-user

### SQLite Store

```go
opts.TapeConfig = tape.StoreConfig{
    Driver:   "sqlite",
    Database: "./tape.db",
}
```

- Single SQLite database with FTS5 full-text search
- Supports: `FetchAll`, `FetchAfter`, `Query` (text search)
- Best for: single-node production, local apps with search

### PostgreSQL Store

```go
opts.TapeConfig = tape.StoreConfig{
    Driver:   "postgres",
    Host:     "localhost",
    Port:     5432,
    Database: "dmr",
    User:     "dmr",
    Password: "secret",
}
```

- Full PostgreSQL backend
- Supports concurrent access
- Best for: multi-node deployments, team usage

---

## Query and Export

### Text Search (SQLite FTS5)

```go
entries, err := store.Query(tapeName, "error handling")
```

### Export

```go
// Export tape to NDJSON
err := tape.Export(store, tapeName, os.Stdout)

// Export all tapes
err := tape.ExportAll(store, outputDir)
```

---

## Anchor Mechanism

Anchors are the key to context window management:

```
Tape entries:
[0] system: "You are a helpful assistant"
[1] user: "Hello"
[2] assistant: "Hi!"
[3] anchor: "Summary: User greeted. Assistant responded."  <-- context starts here
[4] user: "Tell me about Go"
[5] assistant: "Go is..."
[6] user: "What about concurrency?"
[7] assistant: tool_call[search]
[8] tool: "{search results...}"
```

When building context for the next turn:
- Start from entry [3] (latest anchor)
- Include entries [3] through [8]
- The summary in [3] preserves earlier context without full history

Anchors are written during compaction (see [09-compact.md](09-compact.md)).

---

## Tape Isolation

Each `tapeName` corresponds to an isolated conversation:

```go
// Two independent conversations
res1, _ := kit.Agent.Run(ctx, "user-123", "Hello", 0)
res2, _ := kit.Agent.Run(ctx, "user-456", "Hi", 0)
```

In A2A servers, each Task gets its own tape (`TapeModeAuto`):

```go
opts := a2aserver.Options{
    TapeMode:   a2aserver.TapeModeAuto,
    TapePrefix: "a2a",  // tapes: "a2a_task-001", "a2a_task-002", ...
}
```

---

## Working with Tape Directly

```go
// Inspect tape history
entries, _ := kit.Store.FetchAll("my-tape", nil)
for _, e := range entries {
    fmt.Printf("[%d] %s: %s\n", e.ID, e.Role, e.Content)
}

// Resume from specific point
res, _ := kit.Agent.Run(ctx, "my-tape", "Continue", 42)
// Context starts from entry ID 42
```
