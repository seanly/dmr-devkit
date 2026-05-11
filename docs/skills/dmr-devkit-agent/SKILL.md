---
name: dmr-devkit-agent
description: >
  This skill should be used when the user wants to "write agent code",
  "build an agent with devkit", "add a tool", "configure an agent",
  "use devkit.Build", "run an agent", or needs DMR devkit Go API patterns
  and code examples.
  Part of the DMR devkit skills suite.
  Provides a quick reference for Build, Options, Kit, Agent.Run, and common
  configuration patterns.
  Do NOT use for creating new projects (use dmr-devkit-scaffold) or workflow
  orchestration (use dmr-devkit-orchestration).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
    env:
      - AI_MODEL
      - AI_API_KEY
---

# Devkit Agent API Cheatsheet

> **Before using this skill**, activate `/dmr-devkit-workflow` first — it contains the required development phases and scaffolding steps.

## Prerequisites

1. Run `go list -m github.com/seanly/dmr-devkit` — if it errors, run `go get github.com/seanly/dmr-devkit@latest`
2. If no project exists: see `/dmr-devkit-scaffold`
3. If user has existing code: verify `devkit.Build` is used correctly

---

## Quick Reference — Most Common Patterns

### Agent Creation (Minimal)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/seanly/dmr-devkit/devkit"
)

func main() {
    ctx := context.Background()

    opts := devkit.EnvOptions() // reads AI_MODEL, AI_API_KEY, AI_API_BASE from env
    if opts.APIKey == "" || opts.Model == "" {
        log.Fatal("AI_API_KEY and AI_MODEL are required")
    }

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, "Hello, devkit!", 0)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Output)
}
```

### Agent Creation (With Custom Config)

```go
opts := devkit.EnvOptions()
opts.Verbose = 1                           // 0=silent, 1=info, 2=debug, 3=trace
opts.SystemPromptExtra = "Keep answers concise."
opts.MaxSteps = 30                         // default is 20 if 0
opts.Workspace = "/tmp/my-workspace"

kit, err := devkit.Build(ctx, opts)
```

### OAuth2 Authentication (Instead of API Key)

```go
opts := devkit.Options{
    Model:        "gpt-4o",
    APIBase:      "https://api.example.com/v1",
    TokenURL:     "https://auth.example.com/oauth/token",
    ClientID:     "my-client-id",
    ClientSecret: "my-client-secret",
}
```

> **Validation rule:** when any of TokenURL/ClientID/ClientSecret is set, all three are required, plus APIBase.

---

## Options Reference

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `Model` | `string` | Model ID, e.g. `"gpt-4o"`, `"claude-sonnet-4-6"` |
| `APIKey` | `string` | API key (or use OAuth2 below) |

### Authentication

| Field | Type | Description |
|-------|------|-------------|
| `APIBase` | `string` | Custom API base URL |
| `TokenURL` | `string` | OAuth2 token endpoint |
| `ClientID` | `string` | OAuth2 client ID |
| `ClientSecret` | `string` | OAuth2 client secret |
| `Headers` | `map[string]string` | Extra HTTP headers |

### Behavior

| Field | Type | Description |
|-------|------|-------------|
| `ModelName` | `string` | Logical name, default `"default"` |
| `MaxSteps` | `int` | Max agent loop steps, default 20 |
| `Verbose` | `int` | Log level: 0-3 |
| `Workspace` | `string` | Working directory |

### System Prompt

| Field | Type | Description |
|-------|------|-------------|
| `SystemPromptBase` | `string` | Replaces the built-in default prompt entirely |
| `SystemPromptExtra` | `string` | Appended after the default prompt (ignored if Base is set) |

### Storage

| Field | Type | Description |
|-------|------|-------------|
| `TapeStore` | `tape.TapeStore` | Directly pass a store instance |
| `TapeConfig` | `tape.StoreConfig` | Configure driver/dsn/dir |
| `TapeTimezone` | `string` | Timestamp timezone, e.g. `"Asia/Shanghai"` |

### Extension (dmr plugins / custom hooks)

| Field | Type | Description |
|-------|------|-------------|
| `Hooks` | `agent.Hooks` | Extension surface (e.g. `*plugin.Manager` from `github.com/seanly/dmr/pkg/plugin`); default is no-op |
| `OnClose` | `func(context.Context) error` | Run on `Kit.Close` (e.g. `mgr.ShutdownAll`) |

`devkit` does **not** import **dmr**. Build a `plugin.Manager`, `Register` plugins, `InitAll`, then set `opts.Hooks` and `opts.OnClose` before `devkit.Build`. See `/dmr-devkit-plugins`.

---

## Kit Structure

```go
type Kit struct {
    Agent       *agent.Agent       // Runnable agent
    TapeManager *tape.TapeManager  // Conversation history manager
    Store       tape.TapeStore     // Underlying store
    Client      *client.ChatClient // LLM client (advanced use)
    Hooks       agent.Hooks        // Same hooks passed to agent.New (may be NopHooks)
}
```

### Kit.Close

Always defer close so `OnClose` runs (plugin shutdown, etc.):

```go
kit, err := devkit.Build(ctx, opts)
if err != nil {
    log.Fatal(err)
}
defer func() { _ = kit.Close(ctx) }()
```

---

## Agent Execution

### Basic Run

```go
res, err := kit.Agent.Run(ctx, tapeName, prompt, historyAfterEntryID)
```

| Param | Description |
|-------|-------------|
| `ctx` | Go context |
| `tapeName` | Conversation tape name (use `devkit.DefaultTapeName` for single session) |
| `prompt` | User input text |
| `historyAfterEntryID` | Use history after this entry ID (0 = use all) |

### Result

[`agent.RunResult`](https://pkg.go.dev/github.com/seanly/dmr-devkit/agent#RunResult) (alias `Result`):

```go
type RunResult struct {
    Output           string
    Steps            int
    PromptTokens     int
    CompletionTokens int
}
```

### Advanced: RunWithOpts

```go
res, err := kit.Agent.RunWithOpts(ctx, tapeName, prompt, historyAfterEntryID, maxSteps, contextJSON)
```

### Advanced: RunWithOptsAndTools

```go
// Run with a whitelist of allowed tools
res, err := kit.Agent.RunWithOptsAndTools(ctx, tapeName, prompt, historyAfterEntryID, maxSteps, allowedTools, contextJSON)
```

---

## Tape Names and Isolation

Use different tape names to isolate conversation histories:

```go
// Single agent, single session
res, _ := kit.Agent.Run(ctx, "default", "Hello", 0)

// Multi-turn on same tape
res, _ := kit.Agent.Run(ctx, "default", "Follow-up question", 0)

// Isolated sessions
res1, _ := kit.Agent.Run(ctx, "session-alice", "Alice's question", 0)
res2, _ := kit.Agent.Run(ctx, "session-bob", "Bob's question", 0)
```

> Each tape maintains its own conversation history independently.

---

## Environment Variable Cascading

DMR supports layered environment variable resolution. When a value is empty, it falls back:

```go
// Resolution order (first non-empty wins):
// 1. os.Getenv("LITELLM_BASE_URL") / os.Getenv("LITELLM_API_KEY")  — litellm proxy
// 2. os.Getenv("AI_API_BASE") / os.Getenv("AI_API_KEY")            — standard
// 3. model.APIBase / model.APIKey from [[models]] in TOML
// 4. fallback defaults

base := FirstNonEmpty(os.Getenv("LITELLM_BASE_URL"), os.Getenv("AI_API_BASE"))
key := FirstNonEmpty(os.Getenv("LITELLM_API_KEY"), os.Getenv("AI_API_KEY"))
```

**LITELLM compatibility:** When `LITELLM_BASE_URL` is set, the `LITELLM_API_KEY` is used as the key (with fallback to `sk-litellm` if empty).

### Helper: FirstNonEmpty

```go
func FirstNonEmpty(vals ...string) string {
    for _, v := range vals {
        if v != "" {
            return v
        }
    }
    return ""
}
```

## Embedding System Prompts

For large system prompts, use `//go:embed` to load from `.md` files:

```go
package main

//go:embed prompts/my_agent.md
var myAgentPrompt string

// ...
opts.SystemPromptBase = myAgentPrompt
```

Prompt file location:
```
cmd/myagent/
├── main.go
└── prompts/
    └── my_agent.md
```

## Bearer Token Authentication (A2A)

To protect the A2A endpoint with Bearer token auth:

```go
func bearerAuthMiddleware(handler http.Handler, validToken string) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        auth := r.Header.Get("Authorization")
        if auth == "" {
            http.Error(w, "Authorization header required", http.StatusUnauthorized)
            return
        }
        if !strings.HasPrefix(auth, "Bearer ") {
            http.Error(w, "Bearer token required", http.StatusUnauthorized)
            return
        }
        token := strings.TrimPrefix(auth, "Bearer ")
        if token != validToken {
            http.Error(w, "Invalid token", http.StatusUnauthorized)
            return
        }
        handler.ServeHTTP(w, r)
    })
}

// Usage in main:
if bearerToken != "" {
    handler = bearerAuthMiddleware(mux, bearerToken)
}
```

Read the token from config or environment:
```go
bearerToken := os.Getenv("A2A_BEARER_TOKEN")
if spec != nil && spec.A2A.BearerToken != "" {
    bearerToken = spec.A2A.BearerToken
}
```

## Common Gotchas

### App Name / Tape Name Mismatch

When using the `a2aserver` package, **default `TapeModeAuto`** maps each A2A Task to its own DMR tape (`prefix_taskId`). Use `TapeModeFixed` + `TapeName` only if you intentionally share one tape (unsafe under concurrent clients). Match this to any custom agent logic that assumes a specific tape key.

### Forgetting to Close Kit

`OnClose` (e.g. plugin manager shutdown) and other resources may need cleanup. Always defer `kit.Close(ctx)`.

### Zero MaxSteps

`MaxSteps: 0` in Options uses the default (20). If you truly want 0 steps (not recommended), you must set it explicitly after Build.

### Model Not Found Errors

If the LLM API returns a model-not-found error:
1. Verify `AI_MODEL` is set correctly
2. Check if the provider requires a specific model name format
3. Do NOT change the model without asking the user — ask them to confirm the correct model ID

### Integer Parameters Unmarshal as float64

JSON Schema `integer` type is unmarshaled as `float64` in Go. Always use a helper:

```go
func intArg(args map[string]any, key string, defaultVal int) int {
    if v, ok := args[key].(float64); ok {
        return int(v)
    }
    if v, ok := args[key].(int); ok {
        return v
    }
    if v, ok := args[key].(string); ok && v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            return n
        }
    }
    return defaultVal
}
```

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-scaffold` — Project creation and enhancement
- `/dmr-devkit-tools` — Tool development patterns
- `/dmr-devkit-plugins` — Plugin development patterns
- `/dmr-devkit-orchestration` — Workflow orchestration
- `/dmr-devkit-observability` — Storage backends and monitoring
