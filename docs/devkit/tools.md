# 工具开发指南

## 工具概念

在 DMR 中，工具是 Agent 与外部世界交互的基本单元。每个工具包含：

- **ToolSpec** - 宣告式元数据（名称、描述、参数结构），用于生成 LLM 的函数调用描述
- **Handler** - 实际执行逻辑
- **ToolContext** - 运行时上下文（工作空间、Tape 等）

## 工具分组

DMR 支持三种工具加载组：

| 组 | 加载策略 | 说明 |
|----|----------|------|
| `core` | 每轮都加载 | 基础工具，如文件操作、Shell 执行 |
| `extended` | 需要发现 | 扩展工具，通过 `toolSearch` 查询后加载 |
| `mcp` | 需要发现 | MCP 协议工具 |

在 devkit 中通过 `Options.Tools` 注册的工具默认都是 `core` 组。

## 基本工具示例

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/seanly/dmr-devkit/devkit"
    "github.com/seanly/dmr-devkit/tool"
)

func main() {
    ctx := context.Background()
    opts := devkit.EnvOptions()
    opts.APIKey = "your-key"
    opts.Model = "gpt-4o-mini"

    // 注册多个工具
    opts.Tools = []*tool.Tool{
        {
            Spec: tool.ToolSpec{
                Name:        "calculate",
                Description: "执行简单的数学计算",
                Parameters: map[string]any{
                    "type": "object",
                    "properties": map[string]any{
                        "expression": map[string]any{
                            "type":        "string",
                            "description": "要计算的数学表达式，如 1+2*3",
                        },
                    },
                    "required": []any{"expression"},
                },
            },
            Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
                expr, _ := args["expression"].(string)
                // 简单示例：实际应该使用安全的表达式求值器
                result := fmt.Sprintf("已收到表达式: %s（注意：生产环境请使用安全求值器）", expr)
                return map[string]any{"result": result}, nil
            },
        },
        {
            Spec: tool.ToolSpec{
                Name:        "get_time",
                Description: "获取当前时间",
                Parameters: map[string]any{
                    "type":       "object",
                    "properties": map[string]any{},
                },
            },
            Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
                return map[string]any{"time": time.Now().Format(time.RFC3339)}, nil
            },
        },
    }

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, "计算 15*23 并告诉我现在的时间", 0)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Output)
}
```

## ToolContext 使用

`ToolContext` 提供了工具运行时的上下文信息：

```go
type ToolContext struct {
    Ctx        context.Context  // Go 上下文
    Tape       string           // 当前 tape 名称
    RunID      string           // 运行 ID
    Meta       map[string]any   // 元数据
    State      map[string]any   // 状态（含工作空间等）
    Context    map[string]any   // 插件上下文
    Workspace  string           // 工作空间目录
    CwdManager *cwd.Manager     // 当前工作目录管理
}
```

### 访问工作空间

```go
Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
    cwd := ctx.GetCwd()
    // 在工作空间下执行操作
    data, err := os.ReadFile(filepath.Join(cwd, "config.txt"))
    // ...
}
```

### 访问 Tape

```go
Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
    // 从 State 中获取 TapeManager
    if tm, ok := ctx.State[tool.StateKeyTapeManager].(*tape.TapeManager); ok {
        entries, _ := tm.Store.FetchAll(ctx.Tape, nil)
        return map[string]any{"entries_count": len(entries)}, nil
    }
    return nil, fmt.Errorf("无法访问 tape")
}
```

### 访问 RuntimeAgent

```go
Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
    if agent, ok := ctx.State[tool.StateKeyRuntimeAgent].(plugin.RuntimeAgent); ok {
        name, model := agent.GetCurrentModelName(ctx.Tape)
        return map[string]any{"model_name": name, "model": model}, nil
    }
    return nil, fmt.Errorf("无法访问 agent")
}
```

## 动态描述

工具可以在运行时动态生成描述：

```go
{
    Spec: tool.ToolSpec{
        Name:        "list_files",
        Description: "列出目录下的文件", // 默认描述
    },
    DynamicDescription: func(ctx *tool.ToolContext) (string, error) {
        cwd := ctx.GetCwd()
        return fmt.Sprintf("列出 %s 目录下的文件", cwd), nil
    },
    Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
        // ...
    },
}
```

当 `DynamicDescription` 设置且 `ctx` 非空时，会优先使用动态描述。

## 参数验证

DMR 会自动验证工具参数符合 JSON Schema，但你可以在 Handler 中做更严格的检查：

```go
Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
    path, ok := args["path"].(string)
    if !ok || path == "" {
        return nil, fmt.Errorf("参数 path 必填且必须是字符串")
    }
    if strings.Contains(path, "..") {
        return nil, fmt.Errorf("路径不能包含 ..")
    }
    // ...
}
```

## 工具返回格式

工具应返回 JSON 可序列化的数据：

```go
// 基本类型
return "简单字符串", nil

// 地图结构
return map[string]any{
    "status": "ok",
    "data":   []string{"item1", "item2"},
}, nil

// 错误
return nil, fmt.Errorf("操作失败: %w", err)
```

注意：过大的返回值会被自动截断（默认最大 120000 字符），可通过 `AgentPolicy.ToolResultMaxChars` 或 `ModelConfig.ToolResultMaxChars` 调整。
