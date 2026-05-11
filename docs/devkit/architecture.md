# Devkit 架构与设计原理

## 设计目标

Devkit 的核心目标是**在不依赖完整 CLI 的情况下，用最少的代码量构建一个可运行的 LLM Agent**。它不是一个新的框架，而是对 DMR 现有组件的智能装配器。

## 核心组件

Devkit 将以下 DMR 核心组件组合成一个可运行的整体：

```
+-------------------------------------------------------------+
|                         Kit                                 |
|  +-----------+  +-----------+  +----------+  +-----------+  |
|  |   Agent   |  |  Client   |  |  TapeManager |  | Plugins |  |
|  +-----+-----+  +-----+-----+  +----------+  +-----+-----+  |
|        |              |             |              |         |
|        v              v             v              v         |
|  +-----------+  +-----------+  +----------+  +-----------+  |
|  |   Loop    |  | LLMCore   |  |  Store   |  | Registry  |  |
|  | (agent)   |  |(openai)   |  |(tape)    |  | (hooks)   |  |
|  +-----------+  +-----------+  +----------+  +-----------+  |
+-------------------------------------------------------------+
```

### 1. Agent - 多轮对话循环

[`agent` 包](../../agent/agent.go) 实现了核心的 Agent 循环：

- **LLM 调用** -> **工具执行** -> **结果回传** 的自动循环
- 默认最大 20 步（可通过 `MaxSteps` 配置）
- 支持工具发现（Tool Discovery）：延迟加载非核心工具，减少每轮上下文占用
- 自动压缩（Compact/Handoff）：当上下文超过阈值时自动清理历史记录
- 支持每个 Tape 独立的模型切换

### 2. ChatClient - LLM 通信层

[`client` 包](../../client/chat.go) 封装了与 LLM 的交互：

- 支持非流式和流式输出
- 自动工具调用执行
- 从 Tape 读取历史上下文
- 支持系统消息合并优化

### 3. TapeManager - 对话审计轨迹

[`tape` 包](../../tape/manager.go) 提供了对话的持久化存储和查询：

- **存储格式**：基于条目（Entry）的审计轨迹，包括 message、tool_call、tool_result、anchor、event 等
- **上下文窗口**：通过 anchor 机制实现上下文窗口，支持从最近的 anchor 开始读取
- **压缩支持**：支持将历史对话压缩为摘要，释放上下文窗口
- **多后端**：支持内存、文件、SQLite、PostgreSQL、MySQL

### 4. Hooks 与插件（dmr）

公开 **dmr-devkit** 不包含 `plugin` 包。扩展能力通过 [`agent.Hooks`](../../agent/hooks.go) 注入。完整插件栈（`plugin.Plugin`、`HookRegistry`、内置 **`plugins/*`**）由私有仓 **dmr** 的 [`plugin.Manager`](https://github.com/seanly/dmr/blob/main/pkg/plugin/manager.go) 实现，并作为 `devkit.Options.Hooks` / `OnClose` 传入。

- **Hook 机制**：基于命名 Hook 的插件通信（在 **dmr** 中）
- **工具注册**：插件可以注册核心工具或延迟工具
- **策略钩子**：BeforeToolCall、BatchBeforeToolCall 等策略检查
- **系统提示词**：插件可以贡献片段到系统提示词

## 装配原理

### 最小依赖原则

Devkit 不依赖 `~/.dmr/config.toml`、命令行参数或完整的插件生态。只需要 `Model` 和 `APIKey` 两个必填参数即可构建一个可运行的 Agent。

### 组件可替换原则

每个核心组件都可以通过 `Options` 替换：

- `TapeStore` - 替换默认的内存存储
- `Hooks` / `OnClose` - 注入 **dmr** 的 `*plugin.Manager`（或其它 [`agent.Hooks`](../../agent/hooks.go) 实现）
- `Tools` - 注册自定义工具
- `SystemPromptBase` / `SystemPromptExtra` - 定制系统提示词

### 与 CLI 共享核心

Devkit 与 **dmr** 使用相同的 **`agent.Agent`、`client.ChatClient`、`tape.TapeManager`** 源码（本模块内）。**dmr** 在之上装配配置文件、`plugin.Manager` 与 `plugins/*`。这意味着：

- 在嵌入程序中通过 `Hooks` 接入的插件逻辑，可迁移到 **dmr** 的配置化插件列表
- 在同一套核心类型上做的原型与生产行为一致
- 可从 devkit 原型平滑迁移到完整的 `cmd/dmr` 部署

### 环境变量友好

`EnvOptions()` 提供了从标准环境变量读取配置的便捷方式：

- `AI_MODEL` - 模型 ID
- `AI_API_KEY` - API 密钥
- `AI_API_BASE` - API 基础地址

### 合理的默认值

未设置的字段自动使用安全的默认值：

- 未设置 Tape 存储 -> 使用内存存储
- 未设置系统提示词 -> 使用内置默认提示词
- 未设置最大步数 -> 使用 20 步
- 未设置 Hooks -> 使用无操作 [`agent.NopHooks`](../../agent/hooks.go)

## 生命周期

```
1. 构建 (Build)
   ├── 验证 Options
   ├── 创建 TapeStore
   ├── （若使用 **dmr** 插件）在 `devkit.Build` 之前对 `*plugin.Manager` 执行 `Register` + `InitAll`
   ├── 创建 LLMCore
   ├── 创建 TapeManager
   ├── 创建 ToolExecutor（带 Hook 支持）
   ├── 创建 ChatClient
   └── 组装 Agent

2. 运行 (Run)
   ├── Agent.Run() 触发循环
   ├── 每轮自动收集工具
   ├── 工具调用经过 Executor（含 Hook 钩子）
   └── 结果写入 Tape

3. 关闭 (Close)
   └── `Kit.Close` → `Options.OnClose`（例如 `plugin.Manager.ShutdownAll`）
```

## 与其他方案的对比

### Devkit vs cmd/dmr

| 维度 | Devkit | cmd/dmr |
|------|--------|---------|
| 配置方式 | Go 代码 / 环境变量 | `~/.dmr/config.toml` |
| 插件生态 | 手动注册 | 自动加载配置的插件 |
| Tape 存储 | 默认内存 | 默认文件存储 |
| Web/Cron | 不支持 | 内置支持 |
| 适用场景 | 嵌入、原型、测试 | 生产部署 |

### Devkit vs republic

| 维度 | Devkit | republic 包 |
|------|--------|-------------|
| Agent 循环 | 有 | 无 |
| 工具执行 | 有 | 无 |
| 对话历史 | 有 (Tape) | 无 |
| 插件支持 | 有 | 无 |
| 流式输出 | 通过 Client | 原生支持 |
| 适用场景 | 多轮对话+Tool | 单轮 LLM 调用 |
