# L3 — Internal Implementation Details

> Goal: Deep understanding for contributors and advanced customization.  
> Prerequisite: All L1-L2 documents.

---

## Package Dependency Graph

```
devkit (assembly)
    ├── agent (core loop)
    │   ├── client (LLM communication)
    │   │   └── provider/openai (protocol)
    │   ├── tape (storage)
    │   │   ├── file_store
    │   │   ├── sqlite_store
    │   │   └── pg_store
    │   ├── tool (execution)
    │   ├── config (types)
    │   └── core (shared types)
    ├── workflow (orchestration)
    │   ├── compiler
    │   └── dsl
    └── a2aserver (HTTP exposure)

skill (skill management)
    └── tool (delegation)

plugin (capability system)
```

**Key boundary**: `devkit` does not import `dmr`. `dmr` imports `devkit`.

---

## Concurrency Model

### Agent

```
Agent struct {
    mu              sync.RWMutex  // Protects config, chatClients, modelOverrides
    toolsCacheMu    sync.RWMutex  // Protects per-tape tool caches
    onToolCallMu    sync.RWMutex  // Protects OnToolCall callback
    
    // Per-tape maps (safe for concurrent access to different tapes)
    chatClients     map[string]*client.ChatClient
    sessionStarted  map[string]bool
    modelOverrides  map[string]string
    lastCompactStep map[string]int
    discoveredTools map[string]bool
    toolsCache      map[string][]*tool.Tool
}
```

**Concurrency rules**:
- Multiple agents can run on different tapes concurrently
- Same tape should not be run concurrently (history race)
- Hooks methods may be called concurrently (implementations must be thread-safe)

### TapeManager

- Append operations are serialized per tape
- Fetch operations are read-only and concurrent-safe
- SQLite uses connection pooling
- File store uses file locking (`flock`)

---

## Tape Serialization

### File Store Format (NDJSON)

```
{"id":1,"role":"system","content":"You are a helpful assistant"}
{"id":2,"role":"user","content":"Hello"}
{"id":3,"role":"assistant","content":"Hi there!"}
```

### SQLite Schema

```sql
CREATE TABLE entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tape_name TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT,
    tool_calls JSON,
    name TEXT,
    anchor BOOLEAN DEFAULT FALSE,
    event TEXT,
    meta JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_entries_tape ON entries(tape_name, id);
```

### Export Format

```go
// NDJSON export
{"tape":"default","entries":[...]}

// Or per-tape files
<output_dir>/<tape_name>.jsonl
```

---

## Agent Loop State Machine

```
     Start
       │
       ▼
┌─────────────┐
│   Append    │
│   User Msg  │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Build     │
│   Context   │◀────┐
└──────┬──────┘     │
       │            │
       ▼            │
┌─────────────┐     │
│   Call LLM  │     │
└──────┬──────┘     │
       │            │
       ├── Text ────┼──▶ Done
       │            │
       └── Tools ──▶│
              │     │
              ▼     │
       ┌─────────┐  │
       │ Execute │  │
       │  Tools  │  │
       └────┬────┘  │
            │       │
            ▼       │
       ┌─────────┐  │
       │ Append  │──┘
       │ Results │
       └────┬────┘
            │
            ▼
       ┌─────────┐
       │ Check   │── Exceeded? ──▶ Error
       │ MaxSteps│
       └─────────┘
```

---

## Testing Strategy

### Unit Tests

```go
// Test agent loop with in-memory tape
func TestAgentLoop(t *testing.T) {
    client := mock.NewChatClient()
    store := tape.NewMemoryStore()
    tm := tape.NewTapeManager(store)
    agent := agent.New(client, tm, nil, agent.Config{MaxSteps: 5})
    
    res, err := agent.Run(ctx, "test", "Hello", 0)
    require.NoError(t, err)
    assert.NotEmpty(t, res.Output)
}
```

### Mock ChatClient

```go
type MockClient struct {
    Responses []client.Response
    CallCount int
}

func (m *MockClient) Chat(ctx, messages, tools) (*client.Response, error) {
    resp := m.Responses[m.CallCount]
    m.CallCount++
    return &resp, nil
}
```

### Integration Tests

Integration tests use real LLM APIs:

```bash
AI_API_KEY=... AI_MODEL=gpt-4o-mini go test ./agent/... -run Integration
```

---

## Lock Strategy

| Resource | Lock Type | Scope |
|----------|-----------|-------|
| Agent.config | RWMutex | Global |
| Agent.chatClients | RWMutex | Per-map |
| Agent.toolsCache | RWMutex | Per-map |
| TapeStore.Append | Internal | Per-tape |
| FileStore | flock | Per-file |
| SQLite | Connection pool | Global |

---

## Memory Layout

### Agent Per-Tape Caches

```go
// Created on first use, cached for lifetime
type perTapeState struct {
    chatClient   *client.ChatClient  // Cached ChatClient
    tools        []*tool.Tool        // Cached tool list
    model        string              // Model override
    sessionStarted bool              // Whether start anchor written
    lastCompact  int                 // Last compact step number
}
```

### Tool Discovery Cache

```go
// Discovered tools are cached per tape to avoid repeated searches
discoveredTools map[string]bool  // key: "tapeName:toolName"
```

---

## Hot Paths

1. **Agent.Run** → ChatClient.Chat (most frequent)
2. **TapeManager.Append** → Store write (every turn)
3. **ToolExecutor.Execute** → Handler call (when tools invoked)
4. **Hooks.ComposeSystemPrompt** → Every LLM turn

Optimizations:
- Precomputed sorted prompt bases and tape models
- Cached per-tape ChatClients
- Lazy extended tool loading
- Batch hook calls where possible
