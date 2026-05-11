# 工作流编排 (Workflow)

`devkit` 集成了 [`workflow`](../../workflow) 包的编排能力，让你可以将多个 Agent 步骤组合成顺序、并行或图结构的复杂工作流。

## 核心概念

| 类型 | 说明 |
|------|------|
| `Sequential` | 顺序执行，前一个节点的输出作为下一个节点的输入 |
| `Parallel` | 并行执行所有节点，返回有序结果切片 |
| `Graph` | 图结构执行，支持分支路由、并行起始、嵌套子工作流 |
| `AgentNode` | 将 devkit Agent 包装为工作流节点 |

## 顺序工作流

```go
brainstorm := kit.AsAgentNodeWithTape("brainstorm", "wf-brainstorm")
brainstorm.SystemPrompt = "Generate 3 short bullet ideas for the given topic."

drafter := kit.AsAgentNodeWithTape("drafter", "wf-drafter")
drafter.SystemPrompt = "Expand the provided ideas into a short paragraph."

summarizer := kit.AsAgentNodeWithTape("summarizer", "wf-summarizer")
summarizer.SystemPrompt = "Summarize the provided text in one sentence."

seq := &workflow.Sequential{
    WorkflowName: "content_pipeline",
    Nodes:        []workflow.Node{brainstorm, drafter, summarizer},
}

res, err := kit.RunWorkflow(ctx, seq, "The benefits of morning exercise")
```

## 并行工作流

```go
parallel := &workflow.Parallel{
    WorkflowName: "parallel_research",
    Nodes: []workflow.Node{
        kit.AsAgentNodeWithTape("researcher_a", "wf-par-a"),
        kit.AsAgentNodeWithTape("researcher_b", "wf-par-b"),
    },
}
parallel.Nodes[0].(*devkit.AgentNode).SystemPrompt = "List 3 pros of remote work."
parallel.Nodes[1].(*devkit.AgentNode).SystemPrompt = "List 3 cons of remote work."

res, err := kit.RunWorkflow(ctx, parallel, "remote work")
for i, out := range res.Output.([]any) {
    fmt.Printf("Branch %d: %s\n", i+1, out)
}
```

## 图工作流（分支路由）

### 简洁写法（推荐）

使用 `AddConditionalEdges` 自动创建内部路由节点，无需手写 `RouterFunc`：

```go
g := &workflow.Graph{Name: "router"}
g.AddNode("classify", kit.AsAgentNodeWithTape("classify", "wf-classify"))
g.AddNode("handler_a", kit.AsAgentNodeWithTape("handler_a", "wf-a"))
g.AddNode("handler_b", kit.AsAgentNodeWithTape("handler_b", "wf-b"))

g.AddEdge("START", "classify")

// 子串匹配路由 — 自动连边
// "classify" 节点输出包含 "urgent" 时走 handler_a，否则走 handler_b
g.AddConditionalEdges("classify",
    workflow.ContainsRouter(map[string]string{
        "urgent": "urgent",
        "normal": "normal",
    }),
    map[string]string{
        "urgent": "handler_a",
        "normal": "handler_b",
    },
)

res, err := kit.RunWorkflow(ctx, g, "This is an urgent request")
```

### 手动路由（原始写法）

需要显式创建 Router 节点并手动连边：

```go
g.AddEdge("classify", "route")
g.AddRouter("route", func(ctx context.Context, wctx *workflow.Context, input any) (string, any, error) {
    s, _ := input.(string)
    if strings.Contains(s, "urgent") {
        return "urgent", input, nil
    }
    return "normal", input, nil
}, map[string]string{
    "urgent": "handler_a",
    "normal": "handler_b",
})
```

### 预置路由器

| 路由器 | 匹配方式 | 示例 |
|--------|----------|------|
| `ExactMatchRouter` | 完全相等 | `{ "critical": "CRITICAL" }` |
| `ContainsRouter` | 子串包含 | `{ "bug": "BUG", "feat": "feature" }` |
| `PrefixRouter` | 前缀匹配 | `{ "error": "ERROR:" }` |
| `Default(router, fallback)` | 匹配失败时走默认分支 | `Default(ContainsRouter(...), "info")` |

组合使用示例：

```go
g.AddConditionalEdges("classify",
    workflow.Default(
        workflow.ContainsRouter(map[string]string{
            "critical": "CRITICAL",
            "warning":  "WARNING",
        }),
        "info", // 兜底分支
    ),
    map[string]string{
        "critical": "critical_handler",
        "warning":  "warning_handler",
        "info":     "info_handler",
    },
)
```

## 嵌套工作流

工作流可以互相嵌套：Sequential 里放 Parallel，Graph 里放 Sequential：

```go
inner := &workflow.Sequential{
    WorkflowName: "inner",
    Nodes:        []workflow.Node{step1, step2},
}

outer := &workflow.Graph{Name: "outer"}
outer.AddNode("pre", preNode)
outer.AddNode("inner", inner)
outer.AddNode("post", postNode)
outer.AddEdge("START", "pre")
outer.AddEdge("pre", "inner")
outer.AddEdge("inner", "post")

res, err := kit.RunWorkflow(ctx, outer, "input")
```

## 检查点与恢复

`workflow.Context` 内置 `StepLog`，支持从断点恢复：

```go
wctx := workflow.NewContext()
// 模拟：第一个节点已成功执行
wctx.StepLog = append(wctx.StepLog, workflow.LogEntry{
    Step: 0, Node: "brainstorm", Output: "idea1, idea2, idea3",
})

// 再次运行会自动跳过已成功的节点
res, err := seq.Run(ctx, wctx, "topic")
```

## 中断与恢复（Human-in-the-Loop）

节点可以在执行中途调用 `workflow.Interrupt` 暂停工作流，等待外部输入（如人工审批）后再恢复。

```go
approveNode := workflow.NodeFunc{
    N: "approval",
    F: func(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
        draft := input.(string)
        // 第一次运行：抛出 InterruptError，携带 UI  payload
        // 恢复运行：返回用户输入的决策值
        decision, err := workflow.Interrupt(wctx, map[string]any{
            "type":  "approval",
            "draft": draft,
        })
        if err != nil {
            return nil, err
        }
        if decision == "approve" {
            return draft + " [APPROVED]", nil
        }
        return nil, fmt.Errorf("rejected")
    },
}

seq := &workflow.Sequential{
    WorkflowName: "content_pipeline",
    Nodes:        []workflow.Node{writer, approveNode, publisher},
}

// 第一次运行 — 会在 approval 节点中断
res, err := kit.RunWorkflow(ctx, seq, "topic")
if err != nil {
    var ie *workflow.InterruptError
    if errors.As(err, &ie) {
        // 将 wctx.StepLog + wctx.State 持久化到数据库
        // 将 ie.Value 展示给用户，等待输入
    }
}

// 用户点击 "approve" 后恢复
res, err = kit.ResumeWorkflow(ctx, seq, savedWctx, "approve", "topic")
```

恢复时 `ResumeWorkflow` 会自动：
- 注入 `ResumeData` 到上下文
- 重置执行计数器并从 StepLog 回放，已成功的节点自动跳过
- 被中断的节点重新执行，`Interrupt()` 立即返回 `ResumeData`

`workflow.IsInterrupt(err)` 可用于快速判断错误链中是否包含中断。

### 完整示例

参考 [`examples/workflow_interrupt`](../../examples/workflow_interrupt) 可运行示例，展示：
- Writer 节点生成草稿
- Approval 节点调用 `workflow.Interrupt` 暂停并携带 payload
- 模拟人工审批后通过 `ResumeData` 恢复
- 两种路径：approve（成功发布）和 reject（流程失败）

```bash
go run ./examples/workflow_interrupt
```

## 工具共享

同一 `Kit` 创建的所有 `AgentNode` 共享同一套 `Tools`（因为底层是同一个 `Agent`）。如需节点级工具隔离，需要为不同节点创建独立的 `Kit` 实例：

```go
kitWithTools, _ := devkit.Build(ctx, opts)  // 带工具的 Kit
kitPlain, _ := devkit.Build(ctx, plainOpts) // 无工具的 Kit

seq := &workflow.Sequential{
    Nodes: []workflow.Node{
        kitWithTools.AsAgentNodeWithTape("coder", "tape-1"),
        kitPlain.AsAgentNodeWithTape("reviewer", "tape-2"),
    },
}
```

每个节点使用独立的 `TapeName`，对话历史互不干扰。

---

## 事件流（Event Stream）

`workflow` 包支持在执行过程中实时产出事件流，用于 UI 展示、日志追踪或外部监听。所有 Runner（`Sequential`、`Parallel`、`Graph`、`Loop`）都实现了 `EventStream` 接口。

### 事件类型

| 类型 | 触发时机 |
|------|----------|
| `workflow_start` | 工作流开始执行 |
| `workflow_end` | 工作流执行结束（携带最终 `Result`）|
| `node_start` | 节点开始执行 |
| `node_end` | 节点执行结束（携带输出或错误）|
| `node_skip` | 恢复执行时跳过已成功的节点 |
| `interrupt` | 节点调用 `workflow.Interrupt` 暂停 |
| `state_delta` | 状态变更（预留）|

### 基础用法：同步执行（不变）

现有 `RunWorkflow` 调用方式不受影响，内部自动通过事件流消费完成：

```go
res, err := kit.RunWorkflow(ctx, seq, "topic")
```

### 事件流用法：实时观测

使用 `RunWorkflowStream` 消费事件流：

```go
for ev, err := range kit.RunWorkflowStream(ctx, seq, "topic") {
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        break
    }
    switch ev.Type {
    case workflow.EventTypeWorkflowStart:
        fmt.Printf("🚀 Workflow %s started\n", ev.Workflow)
    case workflow.EventTypeNodeStart:
        fmt.Printf("▶️  Step %d: %s\n", ev.Step, ev.Node)
    case workflow.EventTypeNodeEnd:
        if ev.Error != "" {
            fmt.Printf("❌ %s failed: %s\n", ev.Node, ev.Error)
        } else {
            fmt.Printf("✅ %s done\n", ev.Node)
        }
    case workflow.EventTypeNodeSkip:
        fmt.Printf("⏭️  %s skipped (resumed)\n", ev.Node)
    case workflow.EventTypeInterrupt:
        fmt.Printf("⏸️  %s interrupted, waiting for input\n", ev.Node)
    case workflow.EventTypeWorkflowEnd:
        if ev.Result != nil {
            fmt.Printf("🏁 Completed in %d steps\n", ev.Result.Steps)
            fmt.Printf("Output: %v\n", ev.Result.Output)
        }
    }
}
```

### 低层用法：直接调用 Runner 事件流

不通过 devkit，直接使用 `workflow` 包：

```go
seq := &workflow.Sequential{
    WorkflowName: "demo",
    Nodes:        []workflow.Node{stepA, stepB},
}

wctx := workflow.NewContext()
for ev, err := range seq.RunEvents(ctx, wctx, "input") {
    // ... 处理事件
}

// 同步调用仍可用，内部消费同一事件流
res, err := seq.Run(ctx, wctx, "input")
```

### 事件流与中断恢复配合使用

事件流在中断场景下仍然有效，可以实时捕获中断事件：

```go
for ev, err := range kit.RunWorkflowStream(ctx, seq, "topic") {
    if ev.Type == workflow.EventTypeInterrupt {
        // 实时通知 UI 展示审批面板
        payload := ev.Output // 即 InterruptError.Value
        showApprovalUI(payload)
    }
}
```

### 设计要点

- **零破坏**：`Run()` 保持同步 facade，所有现有代码无需修改
- **并发安全**：Parallel 分支在 goroutine 中计算，事件由主 goroutine 统一 yield
- **嵌套透传**：Sequential/Graph/Loop 作为 Parallel 分支时，其内部事件会透传到外层事件流
- **检查点兼容**：`StepLog` resume 机制与事件流并行工作，`node_skip` 事件标记被跳过的节点
