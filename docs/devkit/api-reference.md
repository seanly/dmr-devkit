# API 参考

## 核心函数

### `Build`

```go
func Build(ctx context.Context, opts Options) (*Kit, error)
```

从 `Options` 构建一个完全装配的 `Kit`。这是 devkit 的核心入口。

执行流程：
1. 验证 Options（Model 必填，APIKey 或 OAuth 参数必填）
2. 创建 TapeStore（内存、文件、数据库）
3. 注册并初始化插件
4. 创建 LLMCore、TapeManager、ToolExecutor、ChatClient
5. 组装系统提示词（支持插件贡献片段）
6. 创建并返回 Agent

### `EnvOptions`

```go
func EnvOptions() Options
```

从环境变量读取配置：

- `AI_MODEL` → `Options.Model`
- `AI_API_KEY` → `Options.APIKey`
- `AI_API_BASE` → `Options.APIBase`

空值字段会被跳过，不会覆盖已有的 Options 设置。

## Options 配置

### 必填字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `Model` | `string` | 模型 ID，如 `"gpt-4o"`、`"claude-sonnet-4-6"` |
| `APIKey` | `string` | API 密钥（使用 OAuth 时可省略） |

### 认证配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `APIBase` | `string` | 自定义 API 基础地址 |
| `TokenURL` | `string` | OAuth2 token 端点 |
| `ClientID` | `string` | OAuth2 客户端 ID |
| `ClientSecret` | `string` | OAuth2 客户端秘钥 |
| `Headers` | `map[string]string` | 额外 HTTP 头 |

### 超时配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `HTTPResponseHeaderTimeout` | `int` | 响应头超时（秒），0=默认 10 分钟 |
| `HTTPClientTimeout` | `int` | 整体请求超时（秒），0=默认 15 分钟 |

### 模型与行为

| 字段 | 类型 | 说明 |
|------|------|------|
| `ModelName` | `string` | 逻辑名，默认 `"default"` |
| `MaxSteps` | `int` | 最大循环步数，0=默认 20 |
| `AgentPolicy` | `config.AgentConfig` | 上下文交付策略、Token 限制等 |
| `Verbose` | `int` | 日志详细级别：0=静默，1=info，2=debug，3=trace |

### 系统提示词

| 字段 | 类型 | 说明 |
|------|------|------|
| `SystemPromptBase` | `string` | 完全替换默认系统提示词 |
| `SystemPromptExtra` | `string` | 追加在默认提示词之后（被 SystemPromptBase 覆盖时忽略） |

### 工具与插件

| 字段 | 类型 | 说明 |
|------|------|------|
| `Tools` | `[]*tool.Tool` | 注册为核心工具 |
| `Plugins` | `[]plugin.Plugin` | 要注册的插件实例 |
| `PluginConfigs` | `map[string]map[string]any` | 每个插件的配置数据 |

### Tape 存储

| 字段 | 类型 | 说明 |
|------|------|------|
| `TapeStore` | `tape.TapeStore` | 直接传入存储实例（最高优先级） |
| `TapeConfig` | `tape.StoreConfig` | 存储配置（Driver/DSN/Dir） |
| `TapeTimezone` | `string` | Tape 时间戳时区，如 `"Asia/Shanghai"` |
| `Workspace` | `string` | 工作空间目录，会传递给 Tape 配置 |

## Kit 结构

```go
type Kit struct {
    Agent       *agent.Agent       // 可运行的 Agent
    TapeManager *tape.TapeManager  // Tape 管理器
    Store       tape.TapeStore     // 底层存储实例
    Plugins     *plugin.Manager    // 插件管理器
    Client      *client.ChatClient // LLM 客户端（高级用法）
}
```

### `Kit.Close`

```go
func (k *Kit) Close(ctx context.Context) error
```

关闭所有注册的插件（按注册逆序调用 `Shutdown`）。应该在 Kit 不再需要时调用。

## Agent 运行

### `Agent.Run`

```go
func (a *Agent) Run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32) (*Result, error)
```

执行完整的 Agent 循环，返回最终结果。

- `tapeName` - 对话轨迹名称，用于分隔不同对话
- `prompt` - 用户输入
- `historyAfterEntryID` - 只使用指定 ID 之后的历史记录（0=使用全部）

### `Agent.RunWithOpts`

```go
func (a *Agent) RunWithOpts(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32, maxSteps int, contextJSON string) (*Result, error)
```

支持限制最大步数和传递插件上下文。

### `Agent.RunWithOptsAndTools`

```go
func (a *Agent) RunWithOptsAndTools(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32, maxSteps int, allowedTools *[]string, contextJSON string) (*Result, error)
```

`allowedTools == nil` 表示不按名称限制工具可见性（仍受插件发现、策略等约束）。若传入非空指针，仅允许 `*allowedTools` 中列出的工具名；指针指向的空切片等价于不向模型暴露任何工具。

## 结果结构

```go
type Result struct {
    Output           string // LLM 的最终输出文本
    Steps            int    // 实际执行步数
    PromptTokens     int    // 提示 token 数（如果提供了）
    CompletionTokens int    // 完成 token 数（如果提供了）
}
```

## 常量

```go
const DefaultTapeName = "default"
```

推荐的单会话 Tape 名称。
