# 插件开发指南

`plugin.Plugin`、`HookRegistry` 与 **`plugins/*`** 由私有仓 **dmr** 提供；公开 **dmr-devkit** 仅暴露 [`agent.Hooks`](../../agent/hooks.go)。嵌入 devkit 时，在 `main` 中 **`go get github.com/seanly/dmr`**，用 `plugin.NewManager()` 注册并初始化插件，再传入 `devkit.Options.Hooks` 与 `OnClose`。

> 更完整的英文说明见同目录下 [skills 中的 `dmr-devkit-plugins`](../skills/dmr-devkit-plugins/SKILL.md)。

DMR 的插件系统允许你通过 Hook 机制扩展 Agent 的能力。插件可以：

- 注册新工具（核心或延迟）
- 贡献系统提示词片段
- 实现工具调用前的策略检查
- 拦截用户输入
- 在 Agent 运行结束后执行操作

## 插件接口

所有插件必须实现 `plugin.Plugin` 接口：

```go
type Plugin interface {
    Name() string
    Version() string
    Init(ctx context.Context, config map[string]any) error
    RegisterHooks(registry *plugin.HookRegistry)
    Shutdown(ctx context.Context) error
}
```

## 最小插件示例

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/seanly/dmr/pkg/plugin"
    "github.com/seanly/dmr-devkit/devkit"
    "github.com/seanly/dmr-devkit/tool"
)

// MyPlugin 是一个最小化插件示例
type MyPlugin struct{}

func (p MyPlugin) Name() string    { return "myplugin" }
func (p MyPlugin) Version() string { return "1.0.0" }

func (p MyPlugin) Init(ctx context.Context, config map[string]any) error {
    fmt.Println("插件初始化")
    return nil
}

func (p MyPlugin) Shutdown(ctx context.Context) error {
    fmt.Println("插件关闭")
    return nil
}

func (p MyPlugin) RegisterHooks(r *plugin.HookRegistry) {
    // 注册系统提示词片段
    r.RegisterSystemPrompt(p.Name(), 10, func(ctx context.Context, args ...any) (any, error) {
        return "你是一个有帮助的助手。", nil
    })

    // 注册核心工具
    r.RegisterCoreTools(p.Name(), 10, func(ctx context.Context, args ...any) (any, error) {
        return []*tool.Tool{{
            Spec: tool.ToolSpec{
                Name:        "myplugin_greet",
                Description: "用插件提供的方式问候",
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
                return map[string]any{"greeting": "Hello from plugin, " + name}, nil
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
    if err := mgr.InitAll(ctx, map[string]map[string]any{
        "myplugin": {"greeting_style": "formal", "language": "zh"},
    }); err != nil {
        log.Fatal(err)
    }

    opts := devkit.EnvOptions()
    if opts.APIKey == "" || opts.Model == "" {
        log.Fatal("需要 AI_API_KEY 与 AI_MODEL")
    }
    opts.Hooks = mgr
    opts.OnClose = mgr.ShutdownAll

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        panic(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, "用 myplugin_greet 向 Alice 问好", 0)
    if err != nil {
        panic(err)
    }
    fmt.Println(res.Output)
}
```

## 配置传递

向插件传递配置：在调用 `mgr.InitAll` 时传入 `map[插件名]config`，例如：

```go
if err := mgr.InitAll(ctx, map[string]map[string]any{
    "myplugin": {
        "greeting_style": "formal",
        "language":       "zh",
    },
}); err != nil {
    log.Fatal(err)
}
```

（若尚未 `Register` 对应插件，`InitAll` 仍会跳过未知键。）

```go
func (p MyPlugin) Init(ctx context.Context, config map[string]any) error {
    var cfg struct {
        GreetingStyle string `json:"greeting_style"`
        Language      string `json:"language"`
    }
    if err := plugin.BindConfig(config, &cfg); err != nil {
        return err
    }
    // 使用配置...
    return nil
}
```

## 常用 Hook 说明

### RegisterCoreTools / RegisterExtendedTools

注册工具。核心工具每轮都可用，延迟工具需要通过 `toolSearch` 发现。

```go
r.RegisterCoreTools(p.Name(), 10, func(ctx context.Context, args ...any) (any, error) {
    return []*tool.Tool{{...}}, nil
})
```

### SystemPrompt

贡献系统提示词片段。所有片段会被组合成最终的系统提示词。

```go
r.RegisterSystemPrompt(p.Name(), 10, func(ctx context.Context, args ...any) (any, error) {
    return "你可以访问文件系统。", nil
})
```

### BeforeToolCall

在工具执行前进行策略检查。返回非空错误则拒绝执行。

```go
r.RegisterBeforeToolCall(p.Name(), 10, func(ctx context.Context, args plugin.BeforeToolCallArgs) error {
    // args.Tool 包含工具信息
    // args.Args 包含调用参数
    if args.Tool.(*tool.Tool).Spec.Name == "dangerous_op" {
        return fmt.Errorf("危险操作被禁止")
    }
    return nil
})
```

### InterceptInput

拦截用户输入。如果返回非空结果，则不进入 LLM 循环直接返回结果。

```go
r.RegisterInterceptInput(p.Name(), 10, func(ctx context.Context, args plugin.InterceptInputArgs) (*plugin.InterceptResult, error) {
    if args.Prompt == ",status" {
        return &plugin.InterceptResult{Output: "系统运行正常"}, nil
    }
    return nil, nil // 不拦截，继续正常处理
})
```

### AfterAgentRun

Agent 运行结束后触发。常用于日志记录、SSE 推送等。

```go
r.RegisterAfterAgentRun(p.Name(), 10, func(ctx context.Context, args plugin.AfterAgentRunArgs) error {
    fmt.Printf("对话 %s 结束，执行了 %d 次工具调用\n", args.TapeName, args.ToolIterations)
    return nil
})
```

## 外部插件

DMR 支持通过 gRPC 加载外部进程插件，但这需要完整的 DMR 运行时。在 devkit 中推荐使用内嵌插件。

## 与完整 DMR 的兼容性

在 devkit 中开发的内嵌插件可以直接移植到 `cmd/dmr` 中，只需将插件加入配置文件的 `plugins` 列表。这使得从原型到生产的迁移变得简单。
