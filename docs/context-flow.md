# DMR Context 处理流程

DMR 中 **context（上下文）** 的完整处理流程：从消息写入 tape，到构建 LLM 上下文，再到触发压缩、生成摘要、评审，最后继续下一轮对话。

## 1. 核心原则：Tape 是唯一真相来源

DMR 不维护内存中的对话历史，所有内容按时间顺序追加到 **tape** 中：

| tape entry 类型 | 作用 |
|---|---|
| `message` | 用户/助手的对话消息 |
| `system` / `system_prompt` | 系统提示 |
| `tool_call` / `tool_result` | 工具调用与结果 |
| `task_state` | 结构化任务状态（goal、constraints、pending、active_files） |
| `compact_summary` | 上下文压缩后生成的摘要 |
| `anchor` | 上下文重置/分割点（handoff 或 compact） |
| `event` | 审计事件（loop:compact、loop:handoff 等） |

每次给 LLM 发请求时，agent 都会从 tape **重新读取**并构建上下文，而不是复用上一次的内存状态。

## 2. 给 LLM 构建上下文（每轮对话）

### 2.1 选择 TapeContext

`agent/agent.go:132` 的 `tapeContextForTape` 决定怎么从 tape 里取消息：

```go
if SoftBoundary && KeepBeforeAnchor > 0 {
    ctx = NewSoftBoundaryContext(KeepBeforeAnchor)
} else {
    ctx = NewLastAnchorContext()
}
ctx.SkipPoorSummaries = QualityFallback
ctx.Strategy = strategy
```

- **`LastAnchorContext`**：只取 **最后一个 anchor 之后** 的 entries。
- **`SoftBoundaryContext`**：取最后一个 anchor 之后的内容 **+** anchor 前 `KeepBeforeAnchor` 条原始消息，作为安全网。
- **`SkipPoorSummaries`**：如果启用 `QualityFallback`，质量为 `poor` 的 `compact_summary` 会被跳过。
- **`Strategy`**：决定如何把 tape entries 转成 messages（summary / snip / collapse / hybrid / semantic_collapse）。

### 2.2 读取并转换 entries

`tape/builder.go:23` 的 `ReadMessages` 流程：

1. **FetchEntries**：根据 `TapeContext.AnchorMode` 读取 entries。
   - `LastAnchor`：从最后一个 anchor 读到末尾。
   - `NamedAnchor`：从指定 anchor 读到末尾。
   - `NoAnchor`：读全部。
2. **BuildMessages**：把 entries 转成 LLM message dict。
   - 注入最新的 `task_state` 作为 system message。
   - 注入 `compact_summary` 作为 system message（除非被跳过）。
   - 普通 `message` 直接保留。
   - `anchor`、`event` 等不发给 LLM。
3. **Soft Boundary（可选）**：再往前抓 `KeepBeforeAnchor` 条原始 message/system/tool 消息，追加到上下文末尾。

### 2.3 Token 预估与截断

`agent/loop.go:284`：

```go
tokensEst := a.estimateContextTokens(tapeName, tapeCtx)
```

- 用 token estimator 预估当前上下文 token 数。
- 如果超过 `handoffContextLimit`（由当前模型 `ResolveContextLimit` 决定），会触发 proactive compact。

## 3. 什么时候触发压缩（compact）

有三种触发方式：

### 3.1 自动触发（proactive handoff）

`agent/loop.go:315`：

```go
if estimatedTokens > 0 && a.shouldAutoHandoffByEstimate(tapeName, estimatedTokens) {
    slog.Info("compact: preemptive trigger", ...)
}
```

当预估 token 数超过 `HandoffThreshold * contextLimit` 时，主动 compact。

### 3.2 LLM 调用 handoff 工具

模型可以在 tool 调用中请求 `handoff`，这会：

1. 先 snapshot `task_state`；
2. 然后执行 compact。

### 3.3 用户手动触发

比如 `/handoff 聚焦到社保讨论上` 或代码直接调用 `CompactTape`。

## 4. 压缩（compact）的完整流程

入口：`agent/compact.go:164` `compact()`。

### 4.1 判断是否需要 LLM 摘要

```go
if !a.llmCompactEnabled() {
    // minimal scaffolding：不写摘要，只写 anchor 做上下文重置
}
```

### 4.2 生成摘要

`agent/compact.go:123` `generateCompactSummary()`：

1. **优化 messages**
   - `optimizeEntriesForSummary` / `optimizeMessagesForSummary`
   - 可选 `RollingSummary`（只压缩上次 compact 之后的新消息）
   - 可选 `SemanticCollapse`（把 assistant tool_call + tool_result 折叠）
2. **Token 预算截断**
   - `summarizerInputBudget` 根据模型 context limit 预留 9000 tokens 给 prompt 和输出。
3. **调用 LLM 生成摘要**
   - 用 `structuredCompactPrompt` 要求模型输出 `<summary>...</summary>`。
   - 使用当前 tape 的 chat client（`summarizerChatClient`）。
4. **提取摘要**
   - `extractSummaryTag` 从响应中提取 `<summary>` 内容。

### 4.3 质量评估

`agent/compact.go:27` `evaluateCompactSummary()`：

检查摘要是否包含：

- `task_state.Goal`（权重 2）
- `task_state.Constraints` 的 key/value
- `task_state.Pending` 的 summary
- `task_state.ActiveFiles`

按覆盖率打分：

- `≥ 70%` → **good**
- `≥ 40%` → **fair**
- `< 40%` → **poor**

### 4.4 对抗评审（adversarial judge）

`agent/compact_judge.go`：

现在有两种：

1. **启发式评审（heuristic，默认）**
   - 检查摘要是否包含完整 goal，或 goal 中 ≥4 字符的 token。
   - 对中文不友好，不理解语义。

2. **LLM 语义评审（llm）**
   - 把 `TaskState` 和摘要发给当前模型。
   - 要求输出 JSON：`{"pass": true|false, "reason": "..."}`。
   - 接受同义改写、中文语义等价。
   - 如果 LLM 调用失败，自动降级到启发式评审。

### 4.5 Quality Fallback（可选）

如果 `QualityFallback = true` 且 quality = `poor`：

```go
skipSummary = true
anchorState["fallback_keep_before"] = QualityFallbackKeepBefore
```

- 不写 `compact_summary`。
- 在 anchor 上记录 `fallback_keep_before`。
- 后续上下文构建时，会多保留 anchor 前若干条原始消息。

### 4.6 写入 tape

`tape/manager.go:140` `TapeManager.Compact()`：

1. 读取自上次 anchor 以来的 entries。
2. 转成 messages。
3. 生成/接收 summary。
4. 写入 `anchor` entry（纯标记）。
5. 写入 `compact_summary` entry（包含摘要内容、schema version、quality）。
6. 写入 `event` entry（`compact` 或 `loop:compact`）。

完成后，tape 变成：

```text
... 旧消息 ...
[anchor: compact:20260704-...]
[compact_summary: "..."]
[event: compact]
... 新消息继续追加 ...
```

### 4.7 重置 discovered tools

```go
a.clearDiscoveredToolsWithReason(tapeName, "compact")
```

压缩后通常清空本轮发现的工具（除非 `tool_persistence` 配置保留）。

## 5. 压缩后如何继续对话

下一轮 `tapeContextForTape` 再次执行：

1. 找到最后一个 anchor。
2. 读取 anchor 之后的内容。
3. 注入 `compact_summary` 作为 system message（让模型“回忆”之前发生了什么）。
4. 注入最新的 `task_state` 作为 system message。
5. 继续追加新的用户消息和工具结果。

如果开启了 `SoftBoundary`，还会额外带上 anchor 前几条原始消息，防止摘要质量差导致失忆。

## 6. 关键配置项

都在 `[agent.context]` 下：

```toml
[agent.context]
soft_boundary = true
keep_before_anchor = 3

compact_gap = 3
pressure_override_gap = 1

quality_fallback = true
quality_fallback_keep_before = 8

rolling_summary = true
rolling_summary_full_refresh = 5

semantic_collapse = true
snip_compact = false

compact_strategy = "summary"

# LLM 语义评审（新增）
summary_judge = "llm"          # "heuristic" 或 "llm"
summary_judge_model = ""       # 空 = 使用当前 tape 模型
```

| 配置 | 作用 |
|---|---|
| `soft_boundary` / `keep_before_anchor` | 压缩后保留 anchor 前 N 条原始消息 |
| `compact_gap` | 两次自动 compact 的最小步数间隔 |
| `pressure_override_gap` | token 超过阈值时允许提前 compact 的步数间隔 |
| `quality_fallback` | summary 质量 poor 时丢弃摘要，改保留原始消息 |
| `rolling_summary` | 只压缩上次 compact 后的新增消息 |
| `semantic_collapse` | 折叠 tool_call + tool_result 对 |
| `compact_strategy` | entries 转 messages 的策略 |
| `summary_judge` | 对抗评审方式：heuristic / llm |
| `summary_judge_model` | LLM 评审使用的模型 |

## 7. 完整流程图（简化版）

```text
用户/模型消息 → 追加到 tape
        ↓
每轮 agent loop
        ↓
选择 TapeContext（LastAnchor / SoftBoundary / SkipPoorSummaries）
        ↓
从 tape 读取 entries → 转成 messages
        ↓
预估 token
        ↓
token 超过阈值？ ──是──→ 触发 compact
        ↓ 否              ↓
直接调用 LLM      生成摘要 + 质量评估 + 对抗评审
        ↓              ↓
              写入 anchor + compact_summary + event
        ↓              ↓
        继续下一轮 ←─────┘
```

## 相关源码文件

| 文件 | 职责 |
|---|---|
| `agent/agent.go` | `tapeContextForTape`、模型选择、token budget |
| `agent/loop.go` | 主循环、proactive compact 触发 |
| `agent/compact.go` | compact 编排、摘要生成、质量评估 |
| `agent/compact_judge.go` | 启发式 / LLM 对抗评审 |
| `agent/compact_optimize.go` | message 优化、rolling summary、semantic collapse |
| `agent/loop_events.go` | `loop:compact` 等事件记录 |
| `tape/context.go` | `TapeContext` 定义与默认构建 |
| `tape/builder.go` | entries → messages、soft boundary |
| `tape/manager.go` | `TapeManager.Compact` 写入 anchor/summary/event |
| `handoff/state.go` | `TaskState` 结构 |
| `config/types.go` | `ContextConfig`、`HandoffConfig` |
