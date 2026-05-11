---
name: dmr-devkit-plugins
description: >
  This skill should be used when the user wants to "write a plugin",
  "create a plugin", "add a hook", "extend agent with plugin",
  "Plugin interface", or needs guidance on DMR plugin development.
  Part of the DMR devkit skills suite.
  Covers the Plugin interface, HookRegistry, common hooks, config binding,
  and the transition from devkit to full DMR CLI.
  Do NOT use for tool development (use dmr-devkit-tools) or agent setup
  (use dmr-devkit-agent).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
---

# Devkit Plugin Development Guide

> **Before using this skill**, activate `/dmr-devkit-workflow` first — it contains the required development phases.

## Plugin Concept

DMR's plugin system extends Agent capabilities through a **Hook-based architecture**. Plugins can:

- Register tools (core or extended)
- Contribute system prompt fragments
- Implement policy checks before tool execution
- Intercept user input
- Execute actions after agent runs

## Quick Reference

### Minimal Plugin

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/seanly/dmr-devkit/devkit"
    "github.com/seanly/dmr/pkg/plugin"
    "github.com/seanly/dmr-devkit/tool"
)

type MyPlugin struct{}

func (p MyPlugin) Name() string    { return "myplugin" }
func (p MyPlugin) Version() string { return "1.0.0" }

func (p MyPlugin) Init(ctx context.Context, config map[string]any) error {
    fmt.Println("MyPlugin initialized")
    return nil
}

func (p MyPlugin) Shutdown(ctx context.Context) error {
    fmt.Println("MyPlugin shutdown")
    return nil
}

func (p MyPlugin) RegisterHooks(r *plugin.HookRegistry) {
    // Register a system prompt fragment
    r.RegisterSystemPrompt(p.Name(), 10, func(ctx context.Context, args ...any) (any, error) {
        return "You have access to myplugin tools.", nil
    })

    // Register a core tool
    r.RegisterCoreTools(p.Name(), 10, func(ctx context.Context, args ...any) (any, error) {
        return []*tool.Tool{{
            Spec: tool.ToolSpec{
                Name:        "myplugin_greet",
                Description: "Greet someone using myplugin",
                Parameters: map[string]any{
                    "type": "object",
                    "properties": map[string]any{
                        "name": map[string]any{"type": "string"},
                    },
                    "required": []any{"name"},
                },
            },
            Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
                name, _ := args["name"].(string)
                return map[string]any{"greeting": "Hello from myplugin, " + name}, nil
            },
        }}, nil
    })
}

func main() {
    ctx := context.Background()

    mgr := plugin.NewManager()
    if err := mgr.Register(MyPlugin{}); err != nil {
        log.Fatal(err)
    }
    if err := mgr.InitAll(ctx, nil); err != nil {
        log.Fatal(err)
    }

    opts := devkit.EnvOptions()
    opts.Hooks = mgr
    opts.OnClose = mgr.ShutdownAll

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, "Use myplugin_greet for Alice", 0)
    _ = res
    _ = err
    // ...
}
```

---

## Plugin Interface

```go
type Plugin interface {
    Name() string
    Version() string
    Init(ctx context.Context, config map[string]any) error
    RegisterHooks(registry *HookRegistry)
    Shutdown(ctx context.Context) error
}
```

| Method | Called When | Purpose |
|--------|-------------|---------|
| `Name()` | Registration | Unique plugin identifier |
| `Version()` | Registration | Plugin version |
| `Init()` | `Manager.InitAll` (call **before** `devkit.Build`) | One-time config parsing |
| `RegisterHooks()` | `Manager.Register` (before `InitAll`) | Declare hooks this plugin provides |
| `Shutdown()` | `Kit.Close` → `OnClose` (e.g. `Manager.ShutdownAll`) | Cleanup resources |

---

## Hook Call Strategies

| Strategy | Behavior |
|----------|----------|
| `CallAll` | Invoke all implementations, collect all results |
| `CallFirstResult` | Stop at first non-nil result |
| `CallUntilError` | Stop at first error |

---

## Common Hooks

### RegisterCoreTools / RegisterExtendedTools

```go
r.RegisterCoreTools(p.Name(), priority, func(ctx context.Context, args ...any) (any, error) {
    return []*tool.Tool{{...}}, nil
})
```

- `priority`: Lower number = earlier execution (when order matters)
- Core tools are always available to the agent
- Extended tools require discovery via `toolSearch`

### SystemPrompt

```go
r.RegisterSystemPrompt(p.Name(), priority, func(ctx context.Context, args ...any) (any, error) {
    return "Additional behavior instruction here.", nil
})
```

All registered system prompt fragments are composed into the final prompt.

### BeforeToolCall

```go
r.RegisterBeforeToolCall(p.Name(), priority, func(ctx context.Context, args plugin.BeforeToolCallArgs) error {
    if args.Tool.Spec.Name == "dangerous_op" {
        return fmt.Errorf("dangerous operation blocked by policy")
    }
    return nil
})
```

Strategy: `CallUntilError` — first non-nil error blocks the tool call.

### BatchBeforeToolCall

```go
r.RegisterBatchBeforeToolCall(p.Name(), priority, func(ctx context.Context, args plugin.BatchBeforeToolCallArgs) (any, error) {
    // args.Items contains all pending tool calls
    // Return map[int]error to deny specific calls by index
    return nil, nil
})
```

### InterceptInput

```go
r.RegisterInterceptInput(p.Name(), priority, func(ctx context.Context, args plugin.InterceptInputArgs) (*plugin.InterceptResult, error) {
    if args.Prompt == ",status" {
        return &plugin.InterceptResult{Output: "System operational"}, nil
    }
    return nil, nil // Continue normal processing
})
```

Strategy: `CallFirstResult` — first non-nil result bypasses the LLM.

### AfterAgentRun

```go
r.RegisterAfterAgentRun(p.Name(), priority, func(ctx context.Context, args plugin.AfterAgentRunArgs) error {
    fmt.Printf("Run completed: %d tool calls\n", args.ToolIterations)
    return nil
})
```

---

## Config Binding

Pass per-plugin config to `Manager.InitAll` **before** `devkit.Build` (not via `devkit.Options`):

```go
if err := mgr.InitAll(ctx, map[string]map[string]any{
    "myplugin": {
        "greeting_style": "formal",
        "language":       "zh",
    },
}); err != nil {
    log.Fatal(err)
}
opts := devkit.EnvOptions()
opts.Hooks = mgr
opts.OnClose = mgr.ShutdownAll
kit, err := devkit.Build(ctx, opts)
```

Parse in `Init`:

```go
func (p MyPlugin) Init(ctx context.Context, config map[string]any) error {
    var cfg struct {
        GreetingStyle string `json:"greeting_style"`
        Language      string `json:"language"`
    }
    if err := plugin.BindConfig(config, &cfg); err != nil {
        return err
    }
    // Use cfg...
    return nil
}
```

---

## Devkit vs Full DMR CLI

Plugins developed against `plugin.Manager` + `devkit.Options.Hooks` can migrate to the full `cmd/dmr` CLI without rewriting plugin code:

| Aspect | Devkit embed | Full CLI |
|--------|--------------|----------|
| Registration | `mgr.Register`, `InitAll`, then `opts.Hooks` / `opts.OnClose` | `config.toml` `[[plugins]]` list |
| Config | `InitAll(ctx, map[name]cfg)` | TOML `[plugins.config]` |
| Discovery | Manual wiring | Auto-loaded from config |

---

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| Plugin tools not available | Hook not registered correctly | Verify `RegisterCoreTools` is called in `RegisterHooks` |
| System prompt not applied | Priority too low / fragment empty | Check priority value and returned string |
| BeforeToolCall not firing | Wrong hook name or signature | Use exact type `plugin.BeforeToolCallArgs` |
| Plugin config not parsed | Missing `BindConfig` call | Ensure `Init` parses the config map |
| Shutdown not called | Missing `kit.Close()` | Always defer `kit.Close(ctx)` |

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-agent` — Agent API quick reference
- `/dmr-devkit-tools` — Tool development patterns
- `/dmr-devkit-orchestration` — Workflow orchestration
