---
name: dmr-devkit-orchestration
description: >
  This skill should be used when the user wants to "orchestrate agents",
  "workflow", "Sequential", "Parallel", "Graph", "Loop", "branch routing",
  "conditional workflow", "multi-step agent", "interrupt", "resume",
  "human-in-the-loop", or needs guidance on DMR devkit workflow orchestration.
  Part of the DMR devkit skills suite.
  Covers Sequential, Parallel, Graph, Loop runners, conditional edges,
  built-in routers, interrupt/resume, and event streams.
  Do NOT use for basic agent setup (use dmr-devkit-agent) or tool development
  (use dmr-devkit-tools).
metadata:
  author: seanly
  license: MIT
  version: 0.1.0
  requires:
    go: ">= 1.23"
---

# Devkit Workflow Orchestration Guide

> **Before using this skill**, activate `/dmr-devkit-workflow` first — it contains the required development phases. Load this skill **before** writing any workflow code.

## Runner Decision Matrix

Choose the right runner based on your requirements:

| Scenario | Runner | Key Feature |
|----------|--------|-------------|
| Fixed step pipeline (A → B → C) | `Sequential` | Output of step N is input to step N+1 |
| Parallel analysis / multi-angle | `Parallel` | All steps run concurrently, returns `[]any` |
| Condition-based routing | `Graph` + `AddConditionalEdges` | Branch based on output content |
| Repeat until condition met | `Loop` | Custom termination condition |
| Human approval mid-flow | Any + `workflow.Interrupt` | Pause, persist state, resume later |
| Real-time progress observation | Any + `RunWorkflowStream` | Yield execution events |
| Nested workflows | Any combination | Sequential inside Graph, Parallel inside Sequential, etc. |

> **Do NOT use `Sequential` for branching logic** — use `Graph`. Do NOT use `Parallel` when steps depend on each other — use `Sequential`.

---

## Core Concepts

### AgentNode

Wraps a `devkit.Kit.Agent` into a `workflow.Node`:

```go
brainstorm := kit.AsAgentNodeWithTape("brainstorm", "wf-brainstorm")
brainstorm.SystemPrompt = "Generate 3 bullet ideas for the topic."
```

Each node should use a **dedicated tape** to keep conversation history isolated per step.

### Running a Workflow

```go
seq := &workflow.Sequential{
    WorkflowName: "content_pipeline",
    Nodes:        []workflow.Node{brainstorm, drafter, summarizer},
}

res, err := kit.RunWorkflow(ctx, seq, "The benefits of morning exercise")
```

Result:

```go
type Result struct {
    Output any   // Final output (type depends on runner)
    Steps  int   // Number of steps executed
}
```

---

## Sequential Workflow

```go
step1 := kit.AsAgentNodeWithTape("step1", "wf-1")
step1.SystemPrompt = "Extract keywords from the input."

step2 := kit.AsAgentNodeWithTape("step2", "wf-2")
step2.SystemPrompt = "Expand each keyword into a sentence."

seq := &workflow.Sequential{
    WorkflowName: "pipeline",
    Nodes:        []workflow.Node{step1, step2},
}

res, err := kit.RunWorkflow(ctx, seq, "machine learning")
// res.Output contains the final string from step2
```

---

## Parallel Workflow

```go
parallel := &workflow.Parallel{
    WorkflowName: "parallel_research",
    Nodes: []workflow.Node{
        kit.AsAgentNodeWithTape("pros", "wf-pros"),
        kit.AsAgentNodeWithTape("cons", "wf-cons"),
    },
}
parallel.Nodes[0].(*devkit.AgentNode).SystemPrompt = "List 3 pros."
parallel.Nodes[1].(*devkit.AgentNode).SystemPrompt = "List 3 cons."

res, err := kit.RunWorkflow(ctx, parallel, "remote work")
for i, out := range res.Output.([]any) {
    fmt.Printf("Branch %d: %s\n", i+1, out)
}
```

> `Parallel` returns `[]any` in the same order as `Nodes`. All nodes receive the **same input**.

---

## Graph Workflow (Branching)

### Concise Syntax (Recommended)

Use `AddConditionalEdges` with built-in routers — no manual `RouterFunc` needed:

```go
g := &workflow.Graph{Name: "router"}
g.AddNode("classify", kit.AsAgentNodeWithTape("classify", "wf-classify"))
g.AddNode("handler_a", kit.AsAgentNodeWithTape("handler_a", "wf-a"))
g.AddNode("handler_b", kit.AsAgentNodeWithTape("handler_b", "wf-b"))

g.AddEdge("START", "classify")

// "classify" output contains "urgent" → handler_a, otherwise → handler_b
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

### Built-in Routers

| Router | Match Logic | Example Mapping |
|--------|-------------|-----------------|
| `ExactMatchRouter` | Output == key exactly | `{"critical": "CRITICAL"}` |
| `ContainsRouter` | strings.Contains(output, key) | `{"bug": "BUG", "feat": "feature"}` |
| `PrefixRouter` | strings.HasPrefix(output, key) | `{"error": "ERROR:"}` |
| `Default(router, fallback)` | Use fallback if router returns empty | `Default(ContainsRouter(...), "info")` |

### Manual Router (Advanced)

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

---

## Nested Workflows

Workflows can nest arbitrarily:

```go
inner := &workflow.Sequential{
    WorkflowName: "inner",
    Nodes:        []workflow.Node{step1, step2},
}

outer := &workflow.Graph{Name: "outer"}
outer.AddNode("pre", preNode)
outer.AddNode("inner", inner)     // Sequential inside Graph
outer.AddNode("post", postNode)
outer.AddEdge("START", "pre")
outer.AddEdge("pre", "inner")
outer.AddEdge("inner", "post")
```

---

## Interrupt & Resume (Human-in-the-Loop)

A node can pause workflow execution and wait for external input:

```go
approveNode := workflow.NodeFunc{
    N: "approval",
    F: func(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
        draft := input.(string)
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

// First run — interrupts at "approval"
res, err := kit.RunWorkflow(ctx, seq, "topic")
if err != nil {
    var ie *workflow.InterruptError
    if errors.As(err, &ie) {
        // Persist wctx.StepLog + wctx.State
        // Show ie.Value to user, wait for input
    }
}

// After user clicks "approve":
res, err = kit.ResumeWorkflow(ctx, seq, savedWctx, "approve", "topic")
```

`ResumeWorkflow` automatically:
- Injects `ResumeData` into context
- Replays `StepLog` — already-successful nodes are skipped
- The interrupted node re-executes, and `Interrupt()` returns `ResumeData` immediately

Use `workflow.IsInterrupt(err)` for quick detection:

```go
if workflow.IsInterrupt(err) {
    // Handle interrupt
}
```

---

## Checkpoint & Resume (Without Interrupt)

`workflow.Context` has a `StepLog` for checkpoint recovery:

```go
wctx := workflow.NewContext()
// Simulate: first node already succeeded
wctx.StepLog = append(wctx.StepLog, workflow.LogEntry{
    Step: 0, Node: "brainstorm", Output: "idea1, idea2, idea3",
})

// Re-run skips already-successful nodes
res, err := seq.Run(ctx, wctx, "topic")
```

---

## Event Stream (Real-time Observation)

All runners implement `EventStream` for real-time progress:

```go
for ev, err := range kit.RunWorkflowStream(ctx, seq, "topic") {
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        break
    }
    switch ev.Type {
    case workflow.EventTypeWorkflowStart:
        fmt.Printf("Workflow %s started\n", ev.Workflow)
    case workflow.EventTypeNodeStart:
        fmt.Printf("Step %d: %s\n", ev.Step, ev.Node)
    case workflow.EventTypeNodeEnd:
        if ev.Error != "" {
            fmt.Printf("%s failed: %s\n", ev.Node, ev.Error)
        } else {
            fmt.Printf("%s done\n", ev.Node)
        }
    case workflow.EventTypeNodeSkip:
        fmt.Printf("%s skipped (resumed)\n", ev.Node)
    case workflow.EventTypeInterrupt:
        fmt.Printf("%s interrupted, waiting for input\n", ev.Node)
    case workflow.EventTypeWorkflowEnd:
        fmt.Printf("Completed in %d steps\n", ev.Result.Steps)
    }
}
```

### Event Types

| Type | Trigger |
|------|---------|
| `workflow_start` | Workflow begins |
| `workflow_end` | Workflow completes |
| `node_start` | Node begins execution |
| `node_end` | Node completes (success or error) |
| `node_skip` | Node skipped during resume |
| `interrupt` | Node called `workflow.Interrupt` |

---

## Tool Sharing Between Nodes

All `AgentNode`s created from the same `Kit` share the same `Tools` (same underlying `Agent`). For tool isolation, create separate `Kit` instances:

```go
kitWithTools, _ := devkit.Build(ctx, opts)
kitPlain, _ := devkit.Build(ctx, plainOpts)

seq := &workflow.Sequential{
    Nodes: []workflow.Node{
        kitWithTools.AsAgentNode("coder"),
        kitPlain.AsAgentNode("reviewer"),
    },
}
```

---

## Critical Rules

- **Always use dedicated tape names** per node to avoid conversation history collision
- **Do NOT mutate `wctx.State` from multiple goroutines** during `Parallel` execution
- **Interrupt payload must be serializable** — it will be persisted for resume
- **Nested workflows events bubble up** — a Sequential inside a Parallel will emit events through the Parallel's stream

---

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| Nodes see each other's history | Same tape name | Use unique tape per node |
| Graph hangs / no output | Missing edge or router returns unmatched key | Verify all router return keys have edges |
| Parallel result order wrong | Assuming ordered execution | Parallel results are `[]any` in Node order, not completion order |
| Interrupt not resuming | StepLog not persisted | Save `wctx.StepLog` + `wctx.State` before resuming |
| Event stream missing nested events | Not using `RunWorkflowStream` | Use it instead of `RunWorkflow` for observation |

---

## Related Skills

- `/dmr-devkit-workflow` — Development workflow and operational rules
- `/dmr-devkit-agent` — Agent API quick reference
- `/dmr-devkit-tools` — Tool development patterns
- `/dmr-devkit-plugins` — Plugin development patterns
- `/dmr-devkit-a2a` — A2A service exposure
- `/dmr-devkit-observability` — Storage backends and monitoring
