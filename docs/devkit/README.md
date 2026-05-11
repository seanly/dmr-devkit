# Devkit - 基于 DMR 的 Agent 开发工具包

`devkit`（`github.com/seanly/dmr-devkit/devkit`）提供了一套极简的装配接口，让你能够在自己的 Go 程序中嵌入一个功能完整的 LLM Agent，无需加载配置文件、CLI 或完整的 **dmr** 插件生态。

## 何时使用 Devkit

| 场景 | 推荐方案 |
|------|---------|
| 嵌入 Agent 到自己的 Go 程序 | **devkit** |
| 快速原型验证 | **devkit** |
| 测试需要完整 Agent 循环 | **devkit** |
| 生产级部署（配置、Web、Cron） | `cmd/dmr` |
| 仅需 LLM 客户端（Chat/Stream） | `republic` 包 |

Devkit 的诞生背景之一与 A2A 生态有关：Google ADK 内置的 A2A 实现与社区 `a2a-go` 2.0 协议存在不兼容，无法直接互通。DMR 选择基于 `a2a-go` 2.0 构建 A2A 支持，devkit 则提供了不依赖完整 CLI 的最小化 Agent 装配方案，让你既能嵌入单机 Agent，也能通过 [a2aserver 包](a2a-server.md) 将其暴露为符合社区 A2A 标准的远程服务。

## 快速开始

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

    // 从环境变量读取基础配置
    opts := devkit.EnvOptions()
    if opts.APIKey == "" || opts.Model == "" {
        log.Fatal("需要设置 AI_API_KEY 和 AI_MODEL 环境变量")
    }

    // 注册自定义工具
    opts.Tools = []*tool.Tool{{
        Spec: tool.ToolSpec{
            Name:        "hello",
            Description: "向指定对象问好",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "name": map[string]any{"type": "string", "description": "名字"},
                },
                "required": []any{"name"},
            },
        },
        Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
            name, _ := args["name"].(string)
            return map[string]any{"message": "Hello, " + name}, nil
        },
    }}

    // 构建 Agent
    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    // 运行 Agent
    res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, "用 hello 工具向 World 问好", 0)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(res.Output)
}
```

运行：

```bash
AI_API_KEY=your-key AI_MODEL=gpt-4o-mini go run main.go
```

## 文档索引

- [架构与设计原理](architecture.md) - 理解 devkit 的核心设计思想
- [API 参考](api-reference.md) - `Build`、`Options`、`Kit` 完整 API 说明
- [工具开发](tools.md) - 如何为 Agent 注册和实现工具
- [插件开发](plugins.md) - 如何在 devkit 中使用 DMR 插件
- [工作流编排](workflow.md) - 使用 Sequential、Parallel、Graph 编排多步骤 Agent
- [告警接入工作流](workflow-alerting.md) - 将监控告警接入工作流实现自动分级响应
- [A2A 服务暴露](a2a-server.md) - 将 devkit Agent 暴露为 A2A JSON-RPC 服务
- [更多示例](examples.md) - 持久化存储、多模型切换、OAuth2 等高级用法
