# L2 — Plugins and Extensions

> Goal: Understand how to extend devkit via Hooks and Capabilities.  
> Prerequisite: [01-overview.md](01-overview.md), [02-devkit.md](02-devkit.md)  
> Next: [09-compact.md](09-compact.md) for advanced optimization.

---

## Extension Model

`dmr-devkit` provides two levels of extension:

1. **`agent.Hooks`**: Interface-based extension point for the Agent loop
2. **`plugin` package**: Capability-based plugin system (CapHTTP, etc.)

The devkit repo includes the interface definitions. The full plugin manager implementation lives in the private `dmr` repo.

---

## agent.Hooks Interface

```go
type Hooks interface {
    // System prompt composition
    ComposeSystemPrompt(ctx context.Context, basePrompt, tapeName string) (string, error)

    // Tool discovery
    GetTools(tapeName string) []*tool.Tool
    GetExtendedTools(tapeName string) []*tool.Tool

    // Policy hooks
    BeforeToolCall(ctx context.Context, args BeforeToolCallArgs) error
    BatchBeforeToolCall(ctx context.Context, args BatchBeforeToolCallArgs) error

    // Lifecycle
    AfterAgentRun(ctx context.Context, args AfterAgentRunArgs) error
    OnClose(ctx context.Context) error
}
```

### Usage in devkit

```go
// dmr's plugin.Manager implements agent.Hooks
manager := dmrplugin.NewManager(cfg)

opts := devkit.Options{
    Model:  "gpt-4o",
    APIKey: key,
    Hooks:  manager,
    OnClose: manager.ShutdownAll,
}
kit, _ := devkit.Build(ctx, opts)
```

---

## Hook Points

### ComposeSystemPrompt

Called before each LLM turn to build the final system prompt:

```go
func (h *MyHooks) ComposeSystemPrompt(ctx context.Context, base, tape string) (string, error) {
    // Start with base prompt
    prompt := base
    
    // Append plugin-contributed fragments
    for _, fragment := range h.promptFragments {
        prompt += "\n\n" + fragment
    }
    
    // Add tape-specific context
    if meta := h.getTapeMeta(tape); meta != "" {
        prompt += "\n\n" + meta
    }
    
    return prompt, nil
}
```

The agent precomputes sorted prompt bases and tape models for fast lookup.

### GetTools / GetExtendedTools

Return additional tools registered by plugins:

```go
func (h *MyHooks) GetTools(tape string) []*tool.Tool {
    // Return core tools (always loaded)
    return h.coreTools
}

func (h *MyHooks) GetExtendedTools(tape string) []*tool.Tool {
    // Return extended tools (lazy loaded)
    return h.extendedTools
}
```

### BeforeToolCall

Policy check before executing a tool:

```go
func (h *MyHooks) BeforeToolCall(ctx context.Context, args BeforeToolCallArgs) error {
    // args.ToolName, args.Args, args.TapeName
    
    if args.ToolName == "shell" && !h.isAllowed(args.TapeName) {
        return fmt.Errorf("shell tool not allowed for this tape")
    }
    return nil
}
```

Return an error to block the tool execution. The error is sent back to the LLM.

### AfterAgentRun

Called after each Agent.Run completes:

```go
func (h *MyHooks) AfterAgentRun(ctx context.Context, args AfterAgentRunArgs) error {
    // args.TapeName, args.Turn, args.ToolIterations
    
    // Push to SSE subscribers (webserver)
    h.broadcast(args.TapeName, args)
    return nil
}
```

### OnClose

Cleanup when Kit closes:

```go
func (h *MyHooks) OnClose(ctx context.Context) error {
    return h.shutdown()
}
```

---

## plugin Package (Capability Model)

The `plugin/` package defines a typed capability-registration model:

```go
type Plugin interface {
    Name() string
    Version() string
    Init(ctx context.Context, config map[string]any) error
    Shutdown(ctx context.Context) error
    Capabilities() []Capability
}
```

### Capabilities

Capabilities are interface markers:

```go
type Capability interface {
    CapabilityName() string
}
```

Built-in capabilities:

| Capability | Interface | Purpose |
|------------|-----------|---------|
| `CapHTTP` | `HTTPProvider` | Register HTTP handlers |

### CapHTTP Example

```go
type MyPlugin struct{}

func (p *MyPlugin) Capabilities() []plugin.Capability {
    return []plugin.Capability{plugin.CapHTTP{}}
}

func (p *MyPlugin) HTTPHandlers() map[string]http.Handler {
    return map[string]http.Handler{
        "/myplugin/": http.HandlerFunc(p.handle),
    }
}
```

---

## Registry

The `plugin.Registry` collects and indexes plugins by capability:

```go
registry := plugin.NewRegistry()
registry.Register(myPlugin)

// Discover HTTP providers
httpPlugins := registry.ByCapability(plugin.CapHTTP{})
```

---

## Config Binding

`plugin.BindConfig` converts `map[string]any` to typed structs:

```go
type MyConfig struct {
    Endpoint string `json:"endpoint"`
    Timeout  int    `json:"timeout"`
}

var cfg MyConfig
err := plugin.BindConfig(pluginConfigMap, &cfg)
```

---

## Health Checking

Plugins can implement `HealthChecker`:

```go
type HealthChecker interface {
    HealthCheck(ctx context.Context) error
}
```

The plugin manager periodically checks all health-checking plugins.

---

## When to Use What

| Extension Need | Approach | Location |
|---------------|----------|----------|
| Add custom tools | `agent.Hooks.GetTools` | In your main or a custom Hooks impl |
| Policy enforcement | `agent.Hooks.BeforeToolCall` | Custom Hooks |
| Modify system prompt | `agent.Hooks.ComposeSystemPrompt` | Custom Hooks |
| HTTP endpoints | `plugin.CapHTTP` | Full plugin in dmr |
| Full lifecycle (init/config/shutdown) | `plugin.Plugin` interface | dmr repo |

---

## Minimal Hooks Implementation

```go
type MinimalHooks struct {
    tools []*tool.Tool
}

func (h *MinimalHooks) ComposeSystemPrompt(ctx context.Context, base, tape string) (string, error) {
    return base, nil
}

func (h *MinimalHooks) GetTools(tape string) []*tool.Tool {
    return h.tools
}

func (h *MinimalHooks) GetExtendedTools(tape string) []*tool.Tool {
    return nil
}

func (h *MinimalHooks) BeforeToolCall(ctx context.Context, args agent.BeforeToolCallArgs) error {
    return nil
}

func (h *MinimalHooks) BatchBeforeToolCall(ctx context.Context, args agent.BatchBeforeToolCallArgs) error {
    return nil
}

func (h *MinimalHooks) AfterAgentRun(ctx context.Context, args agent.AfterAgentRunArgs) error {
    return nil
}

func (h *MinimalHooks) OnClose(ctx context.Context) error {
    return nil
}

// Usage
opts := devkit.Options{
    Hooks: &MinimalHooks{tools: myTools},
}
```
