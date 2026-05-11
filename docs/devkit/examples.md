# 更多示例

## 持久化 Tape 存储

默认情况下 devkit 使用内存存储，重启后对话历史丢失。可以通过以下方式持久化：

### 文件存储

```go
opts.TapeConfig = tape.StoreConfig{
    Driver: "file",
    Dir:    "./tapes",
}
```

### SQLite 存储

```go
opts.TapeConfig = tape.StoreConfig{
    Driver: "sqlite",
    DSN:    "./tapes.db",
}
```

### PostgreSQL 存储

```go
opts.TapeConfig = tape.StoreConfig{
    Driver: "pg",
    DSN:    "postgres://user:pass@localhost/dmr?sslmode=disable",
}
```

### 自定义 TapeStore

```go
store := tape.NewInMemoryTapeStore() // 或自己实现 tape.TapeStore
opts.TapeStore = store
```

## OAuth2 认证

对于使用 OAuth2 client_credentials 的提供商（如通过 Keycloak 的 LiteLLM 代理）：

```go
opts := devkit.Options{
    Model:        "gpt-4o",
    APIBase:      "https://litellm.example.com/v1",
    TokenURL:     "https://keycloak.example.com/realms/demo/protocol/openid-connect/token",
    ClientID:     "dmr-client",
    ClientSecret: "secret",
}
```

## 多模型配置

Devkit 支持通过配置多个模型并在运行时切换：

```go
// 在构建时只能指定一个主模型，但可以通过 Agent 切换
kit, err := devkit.Build(ctx, opts)

// 切换模型（在运行时）
err := kit.Agent.SwitchModel("default", "gpt-4o")
```

注意：多模型配置更适合在完整 `cmd/dmr` 中使用，通过 `config.toml` 配置多个模型。

## 自定义系统提示词

### 完全替换

```go
opts.SystemPromptBase = "You are a specialized coding assistant. Only answer programming questions."
```

### 追加片段

```go
opts.SystemPromptExtra = "Always respond in Chinese. Be concise."
```

## 控制最大步数

```go
opts.MaxSteps = 10 // 限制最多 10 步
```

或通过 `AgentPolicy`设置：

```go
opts.AgentPolicy = config.AgentConfig{
    MaxSteps: 10,
}
```

## 工作空间配置

```go
opts.Workspace = "/path/to/workspace"
```

工具可以通过 `ToolContext.GetCwd()` 访问工作空间。

## 完整综合示例

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/seanly/dmr-devkit/devkit"
    "github.com/seanly/dmr-devkit/tape"
    "github.com/seanly/dmr-devkit/tool"
)

func main() {
    ctx := context.Background()

    opts := devkit.EnvOptions()
    if opts.APIKey == "" {
        opts.APIKey = os.Getenv("OPENAI_API_KEY")
    }
    if opts.Model == "" {
        opts.Model = "gpt-4o-mini"
    }

    // 持久化存储
    opts.TapeConfig = tape.StoreConfig{
        Driver: "sqlite",
        DSN:    "dmr.db",
    }

    // 工作空间
    opts.Workspace = "./workspace"
    _ = os.MkdirAll(opts.Workspace, 0755)

    // 系统提示词
    opts.SystemPromptExtra = "You are a helpful file assistant. Always check the workspace before answering."

    // 工具
    opts.Tools = []*tool.Tool{
        {
            Spec: tool.ToolSpec{
                Name:        "list_workspace",
                Description: "List files in the workspace",
                Parameters: map[string]any{
                    "type":       "object",
                    "properties": map[string]any{},
                },
            },
            Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
                entries, err := os.ReadDir(ctx.GetCwd())
                if err != nil {
                    return nil, err
                }
                var files []string
                for _, e := range entries {
                    files = append(files, e.Name())
                }
                return map[string]any{"files": files}, nil
            },
        },
    }

    kit, err := devkit.Build(ctx, opts)
    if err != nil {
        log.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    // 多轮对话
    prompts := []string{
        "What files are in my workspace?",
        "Create a summary of what you found.",
    }

    for _, prompt := range prompts {
        res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, prompt, 0)
        if err != nil {
            log.Printf("错误: %v", err)
            continue
        }
        fmt.Printf("用户: %s\nAgent: %s\n\n", prompt, res.Output)
    }
}
```

## 测试中使用 Devkit

Devkit 非常适合写需要完整 Agent 循环的测试：

```go
func TestAgentWithTools(t *testing.T) {
    ctx := context.Background()
    kit, err := devkit.Build(ctx, devkit.Options{
        Model:  "gpt-4o-mini",
        APIKey: "test-key",
        Tools: []*tool.Tool{{
            Spec: tool.ToolSpec{Name: "mock_tool", ...},
            Handler: func(_ *tool.ToolContext, _ map[string]any) (any, error) {
                return "mock result", nil
            },
        }},
    })
    if err != nil {
        t.Fatal(err)
    }
    defer func() { _ = kit.Close(ctx) }()

    // 测试 Agent 行为...
}
```

使用内存 Tape 存储确保测试之间不会互相干扰。
