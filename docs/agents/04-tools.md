# L2 — Tool System

> Goal: Learn to define, register, and execute tools.  
> Prerequisite: [01-overview.md](01-overview.md)  
> Next: [05-workflow.md](05-workflow.md) for orchestration.

---

## Tool Structure

A tool consists of three parts:

```go
type Tool struct {
    Spec        ToolSpec                                          // Declarative metadata
    Handler     func(ctx *ToolContext, args map[string]any) (any, error)  // Runtime logic
    NeedContext bool                                              // Whether handler needs ToolContext
}
```

### ToolSpec

```go
type ToolSpec struct {
    Name        string
    Description string
    Parameters  map[string]any  // JSON Schema for LLM function calling

    Group      ToolGroup  // core / extended / mcp
    AlwaysLoad bool       // Force core behavior regardless of Group
    SearchHint string     // Keywords for ToolSearch matching

    MaxResultChars int    // Tool result size limit (-1 = unlimited)
}
```

---

## Tool Groups

| Group | Loading Strategy | Use Case |
|-------|-----------------|----------|
| `core` | Loaded every turn | Essential tools (file ops, shell) |
| `extended` | Lazy via ToolSearch | Infrequent tools (database query, API call) |
| `mcp` | Lazy via ToolSearch | MCP protocol tools |

### Group Behavior

```go
// Core tool — always visible to LLM
tool := &tool.Tool{
    Spec: tool.ToolSpec{
        Name: "read_file",
        Group: tool.ToolGroupCore,
    },
}

// Extended tool — only visible after discovery
tool := &tool.Tool{
    Spec: tool.ToolSpec{
        Name: "query_database",
        Group: tool.ToolGroupExtended,
        SearchHint: "sql, database, query, postgres",
    },
}
```

---

## ToolContext

Runtime context passed to handlers:

```go
type ToolContext struct {
    Workspace   string           // Working directory
    TapeName    string           // Current tape name
    TapeManager *tape.TapeManager
    Agent       *agent.Agent     // Parent agent (when NeedContext=true)
    ContextData map[string]any   // Parsed from Run's contextJSON
}
```

---

## Basic Tool Example

```go
func main() {
    opts := devkit.EnvOptions()
    opts.Tools = []*tool.Tool{{
        Spec: tool.ToolSpec{
            Name:        "calculate",
            Description: "Evaluate a math expression",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "expression": map[string]any{
                        "type":        "string",
                        "description": "Math expression like 1+2*3",
                    },
                },
                "required": []any{"expression"},
            },
        },
        Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
            expr, _ := args["expression"].(string)
            // Evaluate expression...
            return map[string]any{"result": result}, nil
        },
    }}
    kit, _ := devkit.Build(ctx, opts)
}
```

---

## Dynamic Descriptions

For tools whose behavior varies by context:

```go
tool := &tool.Tool{
    Spec: tool.ToolSpec{
        Name:        "list_files",
        Description: "List files in workspace", // Static fallback
    },
    DynamicDescription: func(ctx *tool.ToolContext) (string, error) {
        files, _ := os.ReadDir(ctx.Workspace)
        return fmt.Sprintf(
            "List files. Current workspace has %d files: %v",
            len(files), fileNames(files),
        ), nil
    },
}
```

The dynamic description replaces `Spec.Description` when generating the tool list sent to LLM.

---

## Tool Result Handling

### Result Size Limits

`MaxResultChars` controls result handling:

| Value | Behavior |
|-------|----------|
| `0` | Use agent/model default |
| `>0` | Cap result to N runes |
| `-1` | Never externalize (send full result) |

### Externalization

Large results are automatically externalized to disk:

1. Result exceeds `MaxResultChars`
2. Written to workspace as `.dmr/toolresult/<name>.md`
3. Tape stores a reference entry
4. LLM receives truncated preview + file path

This is managed by `agent/toolresult/` package.

---

## ToolExecutor

`tool/executor.go` orchestrates tool execution:

```go
type ToolExecutor struct {
    tools      map[string]*Tool
    groups     map[ToolGroup][]*Tool
    // ...
}

func (te *ToolExecutor) Register(tools ...*Tool)
func (te *ToolExecutor) Execute(ctx *ToolContext, calls []ToolCall) ([]ToolResult, error)
```

### Execution Flow

1. Receive `tool_calls` from LLM
2. For each call:
   a. Resolve tool by name
   b. Parse arguments against JSON Schema
   c. Call `BeforeToolCall` hooks
   d. Execute handler
   e. Apply result limits / externalization
   f. Call `AfterToolCall` hooks
3. Return results to Agent loop

---

## Built-in Tools

### toolSearch

Discovers extended/MCP tools on demand:

```go
// Agent automatically calls this when LLM mentions unknown tools
result := toolSearch("database query")
// Returns matching tools with SearchHint containing "database" or "query"
```

### vision

Image analysis tool (when model supports vision):

```go
// Typically invoked with image URLs or base64 data
visionTool := tools.VisionTool(model)
```

Located in `tools/vision/`.

---

## Error Handling in Handlers

```go
Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
    path, _ := args["path"].(string)
    data, err := os.ReadFile(path)
    if err != nil {
        // Return structured error — LLM will see this and may retry
        return nil, fmt.Errorf("failed to read %s: %w", path, err)
    }
    return string(data), nil
},
```

Returned errors are formatted and sent back to the LLM as `tool_result` entries. The LLM can then decide to retry with corrected arguments.
