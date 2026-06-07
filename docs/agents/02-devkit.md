# L2 — Devkit Guide: Build, Options, Kit

> Goal: Learn to wire up and configure a runnable Agent.  
> Prerequisite: [01-overview.md](01-overview.md)  
> Next: [03-agent-loop.md](03-agent-loop.md) for loop internals, [04-tools.md](04-tools.md) for tool development.

---

## devkit.Build

`devkit.Build(ctx, opts)` is the single entry point to assemble a fully wired `*Kit`.

### Execution Flow

1. Validate Options (Model required, APIKey or OAuth required)
2. Create TapeStore (memory, file, or database)
3. Create LLMCore, TapeManager, ToolExecutor, ChatClient
4. Compose system prompt (supports plugin fragment injection)
5. Create and return Agent

```go
kit, err := devkit.Build(ctx, devkit.Options{
    Model:  "gpt-4o",
    APIKey: os.Getenv("AI_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}
defer kit.Close(ctx)
```

### Kit Lifecycle

```go
type Kit struct {
    Agent       *agent.Agent
    TapeManager *tape.TapeManager
    Store       tape.TapeStore
    Client      *client.ChatClient
    Hooks       agent.Hooks
}

// Close runs OnClose from Options if set (e.g. plugin manager shutdown)
kit.Close(ctx)
```

---

## Options

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `Model` | `string` | Model ID, e.g. `"gpt-4o"`, `"claude-sonnet-4-6"` |
| `APIKey` | `string` | API key (optional when using OAuth) |

### Authentication

| Field | Type | Description |
|-------|------|-------------|
| `APIBase` | `string` | Custom API base URL |
| `TokenURL` | `string` | OAuth2 token endpoint |
| `ClientID` | `string` | OAuth2 client ID |
| `ClientSecret` | `string` | OAuth2 client secret |
| `Headers` | `map[string]string` | Extra HTTP headers |

### Storage Configuration

| Field | Type | Description |
|-------|------|-------------|
| `TapeStore` | `tape.TapeStore` | Pre-created store instance (highest priority) |
| `TapeConfig` | `tape.StoreConfig` | Store factory config |

**Priority**: `TapeStore` > `TapeConfig` > in-memory fallback

Store config examples:

```go
// File store
opts.TapeConfig = tape.StoreConfig{
    Driver:    "file",
    Workspace: "/path/to/workspace",
}

// SQLite store
opts.TapeConfig = tape.StoreConfig{
    Driver:   "sqlite",
    Database: "/path/to/tape.db",
}

// PostgreSQL store
opts.TapeConfig = tape.StoreConfig{
    Driver:   "postgres",
    Host:     "localhost",
    Port:     5432,
    Database: "dmr",
    User:     "dmr",
    Password: "secret",
}
```

### System Prompt

| Field | Type | Description |
|-------|------|-------------|
| `SystemPromptExtra` | `string` | Appended after default system prompt |
| `SystemPromptBase` | `string` | Replaces default+extra merge |

```go
// Append to default
opts.SystemPromptExtra = "Keep answers concise. Speak in Chinese."

// Full replacement
opts.SystemPromptBase = "You are a code reviewer. Be strict."
```

### Other Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxSteps` | `int` | 20 | Max loop steps per run |
| `Verbose` | `int` | 0 | Log verbosity (0=warn, 1=info, 2=debug) |
| `Workspace` | `string` | "" | Working directory passed to tools |
| `Tools` | `[]*tool.Tool` | nil | Core tools to register |
| `Hooks` | `agent.Hooks` | nil | Extension hooks (e.g. dmr plugin.Manager) |
| `OnClose` | `func(context) error` | nil | Cleanup callback |
| `Models` | `[]config.ModelConfig` | nil | Multi-model configuration |
| `HTTPResponseHeaderTimeout` | `int` | 0 (10min) | Response header timeout (seconds) |
| `HTTPClientTimeout` | `int` | 0 (15min) | Overall request timeout (seconds) |

---

## EnvOptions

`devkit.EnvOptions()` reads configuration from environment variables:

| Variable | Maps To |
|----------|---------|
| `AI_MODEL` | `Options.Model` |
| `AI_API_KEY` | `Options.APIKey` |
| `AI_API_BASE` | `Options.APIBase` |

Empty values are skipped and do not override existing Options.

```go
opts := devkit.EnvOptions()
opts.Verbose = 1
opts.Tools = myTools
kit, err := devkit.Build(ctx, opts)
```

---

## Running the Agent

### Basic Run

```go
res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, "Hello", 0)
fmt.Println(res.Output)
```

### With History Position

```go
// Resume from entry ID 42
res, err := kit.Agent.Run(ctx, "my-tape", "Continue", 42)
```

### With Context

```go
// Pass JSON context to tools
res, err := kit.Agent.RunWithContext(ctx, tape, prompt, 0, `{"user_id":123}`)
```

### With Tool Whitelist

```go
// Only allow specific tools
allowed := []string{"read_file", "write_file"}
res, err := kit.Agent.RunWithOptsAndTools(ctx, tape, prompt, 0, 0, &allowed, "")
```

---

## Workflow Integration

Devkit provides convenience methods for workflow integration:

### AgentNode

```go
// Create a workflow node wrapping the agent with a dedicated tape
node := kit.AsAgentNodeWithTape("writer", "wf-writer")
node.SystemPrompt = "You are a technical writer."

// Use in Sequential workflow
seq := &workflow.Sequential{
    WorkflowName: "content_pipeline",
    Nodes: []workflow.Node{node},
}

res, err := kit.RunWorkflow(ctx, seq, "topic")
```

### RunWorkflow

```go
// Execute any workflow runner
result, err := kit.RunWorkflow(ctx, runner, input)
fmt.Printf("Completed in %d steps\n", result.Steps)
```

---

## Complete Examples

### Minimal Agent with Tools

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/seanly/dmr-devkit/devkit"
    "github.com/seanly/dmr-devkit/tool"
)

func main() {
    ctx := context.Background()
    opts := devkit.EnvOptions()
    if opts.APIKey == "" || opts.Model == "" {
        log.Fatal("AI_API_KEY and AI_MODEL required")
    }

    opts.Tools = []*tool.Tool{{
        Spec: tool.ToolSpec{
            Name:        "echo",
            Description: "Echo a message back.",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "message": map[string]any{"type": "string"},
                },
                "required": []any{"message"},
            },
        },
        Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
            return args["message"], nil
        },
    }}

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer kit.Close(ctx)

    res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName,
        "Echo 'hello world'", 0)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Output)
}
```

### With Persistent Storage

```go
opts := devkit.EnvOptions()
opts.TapeConfig = tape.StoreConfig{
    Driver:    "sqlite",
    Database:  "./tape.db",
    Workspace: "./workspace",
}
kit, err := devkit.Build(ctx, opts)
```

### With dmr Plugin Manager

```go
// dmr's plugin.Manager implements agent.Hooks
manager := plugin.NewManager(...)

opts := devkit.EnvOptions()
opts.Hooks = manager
opts.OnClose = manager.ShutdownAll

kit, err := devkit.Build(ctx, opts)
```

---

## When to Use What

| Approach | Best For |
|----------|----------|
| **devkit** | Embedding in your own program; quick prototypes; tests needing full loop |
| **dmr** (`cmd/dmr`) | Production: config file, workspace, tape drivers, packaged plugins, webserver |
| **`republic`** | Tape-first **LLM client only** — no multi-step loop or hooks. See `examples/basic_demo.go` |
