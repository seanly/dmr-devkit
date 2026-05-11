---
name: dmr-devkit-tools
description: >
  This skill should be used when the user wants to "write a tool",
  "add a tool", "create a custom tool", "implement a tool handler",
  "ToolSpec", or needs guidance on DMR devkit tool development.
  Part of the DMR devkit skills suite.
  Covers ToolSpec, Handler, ToolContext, parameter schemas, dynamic descriptions,
  and validation.
  Do NOT use for agent setup (use dmr-devkit-agent) or plugin development
  (use dmr-devkit-plugins).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
---

# Devkit Tool Development Guide

> **Before using this skill**, activate `/dmr-devkit-workflow` first — it contains the required development phases.

## Tool Concept

A **Tool** is the basic unit of interaction between an Agent and the external world. Each tool consists of:

- **ToolSpec** — Declarative metadata (name, description, parameter schema) for LLM function calling
- **Handler** — The actual execution logic
- **ToolContext** — Runtime context (workspace, tape, agent access)

## Quick Reference

### Basic Tool

```go
import "github.com/seanly/dmr-devkit/tool"

opts.Tools = []*tool.Tool{{
    Spec: tool.ToolSpec{
        Name:        "get_weather",
        Description: "Get current weather for a city.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "city": map[string]any{
                    "type":        "string",
                    "description": "City name, e.g. Beijing",
                },
            },
            "required": []any{"city"},
        },
    },
    Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
        city, _ := args["city"].(string)
        return map[string]any{
            "city":        city,
            "temperature": "22°C",
            "condition":   "sunny",
        }, nil
    },
}}
```

### Tool with Context Access

```go
Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
    // Access current working directory
    cwd := ctx.GetCwd()

    // Access tape manager (if injected into state)
    if tm, ok := ctx.State[tool.StateKeyTapeManager].(*tape.TapeManager); ok {
        entries, _ := tm.Store.FetchAll(ctx.Tape, nil)
        return map[string]any{"history_count": len(entries)}, nil
    }

    // Access runtime agent
    if agent, ok := ctx.State[tool.StateKeyRuntimeAgent].(plugin.RuntimeAgent); ok {
        name, model := agent.GetCurrentModelName(ctx.Tape)
        return map[string]any{"model": model, "name": name}, nil
    }

    return nil, fmt.Errorf("tool execution failed")
}
```

---

## ToolContext Fields

```go
type ToolContext struct {
    Ctx        context.Context  // Go context
    Tape       string           // Current tape name
    RunID      string           // Run ID
    Meta       map[string]any   // Metadata
    State      map[string]any   // State (includes workspace, etc.)
    Context    map[string]any   // Plugin context
    Workspace  string           // Workspace directory
    CwdManager *cwd.Manager     // Current working directory manager
}
```

### Common Methods

| Method | Description |
|--------|-------------|
| `ctx.GetCwd()` | Get current working directory |
| `ctx.State[tool.StateKeyTapeManager]` | Access TapeManager |
| `ctx.State[tool.StateKeyRuntimeAgent]` | Access RuntimeAgent |

---

## Parameter Schema

The `Parameters` field follows JSON Schema (subset):

```go
Parameters: map[string]any{
    "type": "object",
    "properties": map[string]any{
        "path": map[string]any{
            "type":        "string",
            "description": "File path relative to workspace",
        },
        "limit": map[string]any{
            "type":        "integer",
            "description": "Maximum number of lines to read",
            "default":     100,
        },
    },
    "required": []any{"path"},
}
```

**Supported types:** `string`, `integer`, `number`, `boolean`, `array`, `object`

**Tips:**
- Always provide `description` — the LLM uses it to decide when to call the tool
- Mark truly required fields in `required`
- Use `default` for optional fields with sensible defaults

---

## Dynamic Description

Tools can generate descriptions at runtime based on context:

```go
{
    Spec: tool.ToolSpec{
        Name:        "list_files",
        Description: "List files in a directory", // fallback
    },
    DynamicDescription: func(ctx *tool.ToolContext) (string, error) {
        cwd := ctx.GetCwd()
        return fmt.Sprintf("List files in %s", cwd), nil
    },
    Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
        // ...
    },
}
```

When `DynamicDescription` is set and `ctx` is non-nil, it takes precedence over the static `Description`.

---

## Validation Best Practices

DMR validates parameters against the JSON Schema automatically, but you should add semantic validation in Handler:

```go
Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
    path, ok := args["path"].(string)
    if !ok || path == "" {
        return nil, fmt.Errorf("path is required and must be a string")
    }
    if strings.Contains(path, "..") {
        return nil, fmt.Errorf("path cannot contain '..'")
    }
    // ... execute
}
```

### Integer Parameters Unmarshal as float64

JSON Schema `integer` type is unmarshaled as `float64` in Go. Always use a helper to extract integer arguments:

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

func strArg(args map[string]any, key string, defaultVal string) string {
    if v, ok := args[key].(string); ok {
        return v
    }
    return defaultVal
}
```

Usage in Handler:

```go
Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
    keyword := strArg(args, "keyword", "")
    limit := intArg(args, "limit", 20)

    if keyword == "" {
        return nil, fmt.Errorf("keyword is required")
    }
    if limit <= 0 || limit > 100 {
        limit = 20
    }
    // ...
}
```

---

## Return Values

Return JSON-serializable data:

```go
// Simple value
return "operation completed", nil

// Structured result
return map[string]any{
    "status": "ok",
    "items":  []string{"a", "b", "c"},
    "count":  3,
}, nil

// Error
return nil, fmt.Errorf("operation failed: %w", err)
```

> **Size limit:** Large results are automatically truncated (default max 120000 chars). Adjust via `AgentPolicy.ToolResultMaxChars` or `ModelConfig.ToolResultMaxChars`.

---

## Tool Groups

DMR supports three loading strategies:

| Group | Loading Strategy | Description |
|-------|-----------------|-------------|
| `core` | Always loaded | Basic tools registered via `Options.Tools` |
| `extended` | On-demand discovery | Tools found via `toolSearch` |
| `mcp` | On-demand discovery | MCP protocol tools |

Tools passed via `Options.Tools` are automatically `core` group.

---

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| Tool never called by agent | Description too vague / parameters unclear | Improve `Description` and parameter descriptions |
| Tool called with wrong args | Schema mismatch | Verify `required` fields and parameter types |
| `nil pointer` in Handler | Missing nil check on `ctx` or args | Always validate inputs before dereferencing |
| Result too large truncated | Exceeds ToolResultMaxChars | Return summaries instead of full data |
| `GetCwd()` returns wrong path | Workspace not set | Pass `Workspace` in `devkit.Options` |

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-agent` — Agent API quick reference
- `/dmr-devkit-plugins` — Plugin development (for hook-based tool registration)
- `/dmr-devkit-orchestration` — Workflow orchestration
