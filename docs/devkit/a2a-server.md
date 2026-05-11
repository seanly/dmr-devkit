# A2A 服务暴露指南

## A2A 协议

A2A (Agent2Agent) 是 Google 推出的开放协议，用于不同 Agent 之间的互操作。DMR 基于 `a2a-go` 2.0 实现了 A2A 支持。

## 为什么选择 Devkit + A2A

Google ADK 内置的 A2A 实现与社区 `a2a-go` 2.0 存在不兼容：
- ADK 的 A2A 是其内部实现，与社区标准脱节
- `a2a-go` 2.0 是社区推动的独立实现

Devkit 提供了一个轻量级方案：在自己的 Go 程序中搭建 Agent，通过 `a2aserver` 包暴露为符合社区 A2A 标准的服务。

## 基础示例

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"

    "github.com/seanly/dmr-devkit/a2aserver"
    "github.com/seanly/dmr-devkit/devkit"
)

func main() {
    ctx := context.Background()

    // 1. 用 devkit 构建 Agent
    opts := devkit.EnvOptions()
    if opts.APIKey == "" || opts.Model == "" {
        log.Fatal("需要 AI_API_KEY 和 AI_MODEL")
    }
    opts.Verbose = 1
    opts.SystemPromptExtra = "You are reachable via the A2A protocol. Keep replies concise."

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    // 2. 配置 A2A 服务
    addr := ":8080"
    publicURL := os.Getenv("A2A_PUBLIC_INVOKE_URL")
    if publicURL == "" {
        publicURL = "http://127.0.0.1" + addr + "/invoke"
    }

    mux := http.NewServeMux()
    if err := a2aserver.Mount(mux, a2aserver.Options{
        AgentName:       "dmr-devkit",
        Description:     "DMR agent exposed via a2aserver",
        PublicInvokeURL: publicURL,
        MountPath:       "/invoke",
        // 默认 TapeMode 为 auto：每个 A2A Task 对应独立 tape（见下表）。
    }, kit.Agent); err != nil {
        log.Fatal(err)
    }

    // 3. 启动服务
    log.Printf("A2A listen %s", addr)
    log.Printf("Agent card: http://127.0.0.1%s%s", addr, a2aserver.WellKnownAgentCardPath)
    log.Printf("Invoke URL: %s", publicURL)
    log.Fatal(http.ListenAndServe(addr, mux))
}
```

运行：

```bash
AI_API_KEY=your-key AI_MODEL=gpt-4o-mini go run main.go
```

## 配置说明

### `a2aserver.Options`

| 字段 | 必填 | 说明 |
|------|------|------|
| `AgentName` | 是 | Agent 名称 |
| `Description` | 否 | Agent 描述 |
| `PublicInvokeURL` | 是 | 客户端访问的绝对 URL |
| `MountPath` | 否 | JSON-RPC 路径，默认 `/invoke` |
| `TapeMode` | 否 | `auto`（默认）或 `fixed`：见下方 tape 策略 |
| `TapePrefix` | 否 | `auto` 模式下扁平 tape 名前缀，默认 `a2a`，形如 `a2a_<taskId>` |
| `TapeName` | 否 | **仅 `fixed` 模式**：所有请求共用该 tape，默认 `default`（并发多客户端时易导致对话交错） |
| `DefaultInputModes` | 否 | 默认输入模式 |
| `DefaultOutputModes` | 否 | 默认输出模式 |

### Tape 策略

- **auto（默认）**：用 `TapePrefix` 与请求上下文中的 **A2A TaskID** 组成单一安全片段（无子目录），每条远端 Task 的 DMR 会话历史彼此隔离。
- **fixed**：与旧版行为一致，所有 `SendMessage` 使用同一 `TapeName`。

示例程序 [examples/a2a_devkit_server/main.go](../../examples/a2a_devkit_server/main.go) 支持环境变量：`A2A_TAPE_MODE=fixed`、`A2A_TAPE_NAME`、`A2A_TAPE_PREFIX`（非 fixed 时覆盖前缀）。

### 环境变量

| 变量 | 说明 |
|------|------|
| `A2A_PUBLIC_INVOKE_URL` | 公网可访问的 invoke URL。在 NAT 或 TLS 终结时必填 |

## 从其他 DMR 实例调用

在另一个 DMR 实例中，通过 A2A 插件连接：

```toml
[[plugins]]
name = "a2a"
enabled = true
[plugins.config]
[[plugins.config.agents]]
name = "devkit"
base_url = "http://your-server:8080"
```

然后在对话中使用 `a2a_<name>` 工具调用远程 Agent（默认会在同一 DMR tape 下续传远端 Task，见 **dmr** 文档中的 [A2A 插件说明](https://github.com/seanly/dmr/blob/main/docs/plugins/a2a.md)）。

## 安全提示

- 生产环境应该使用 HTTPS
- 通过反向代理或负载均衡处理 TLS 终结
- 设置 `A2A_PUBLIC_INVOKE_URL` 确保客户端能正确访问
