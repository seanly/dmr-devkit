# L2 — Workflow Orchestration

> Goal: Learn to orchestrate multi-step agent tasks.  
> Prerequisite: [01-overview.md](01-overview.md), [02-devkit.md](02-devkit.md)  
> Related: [04-tools.md](04-tools.md) for tool development.

---

## Overview

The `workflow` package (`workflow/`) provides deterministic orchestration of agent tasks using three execution patterns:

- **Sequential**: Steps execute in order, previous output feeds next input
- **Parallel**: Branches execute concurrently, results aggregated
- **Graph**: Directed graph with conditional edges, supports loops

All workflow types implement the `Runner` interface and can be nested.

---

## Node Interface

```go
type Node interface {
    Name() string
    Run(ctx context.Context, wctx *Context, input any) (any, error)
}
```

### NodeFunc Adapter

```go
node := workflow.NodeFunc{
    N: "doubler",
    F: func(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
        n := input.(int)
        return n * 2, nil
    },
}
```

---

## Sequential

```go
type Sequential struct {
    WorkflowName string
    Nodes        []Node
}
```

Execution: `input → Node[0] → output → Node[1] → ... → final output`

```go
seq := &workflow.Sequential{
    WorkflowName: "content_pipeline",
    Nodes: []workflow.Node{
        kit.AsAgentNodeWithTape("brainstorm", "wf-brain"),
        kit.AsAgentNodeWithTape("draft",      "wf-draft"),
        kit.AsAgentNodeWithTape("polish",     "wf-polish"),
    },
}

result, _ := kit.RunWorkflow(ctx, seq, "topic: AI agents")
// result.Output = polished content
```

Each node gets its own tape for isolated history.

---

## Parallel

```go
type Parallel struct {
    WorkflowName string
    Nodes        []Node
}
```

Execution: All nodes run concurrently with the same input. Results are collected as `[]any`.

```go
par := &workflow.Parallel{
    WorkflowName: "research",
    Nodes: []workflow.Node{
        kit.AsAgentNodeWithTape("pros_researcher",   "wf-pros"),
        kit.AsAgentNodeWithTape("cons_researcher",   "wf-cons"),
        kit.AsAgentNodeWithTape("neutral_researcher", "wf-neu"),
    },
}

result, _ := kit.RunWorkflow(ctx, par, "remote work")
// result.Output = []any{pros_output, cons_output, neutral_output}
```

---

## Graph

```go
type Graph struct {
    WorkflowName string
    Nodes        map[string]Node
    Edges        []Edge
}

type Edge struct {
    From     string
    To       string
    Condition func(output any) bool  // Optional; nil = unconditional
}
```

Supports:
- Conditional branching
- Cycles (loops)
- Multiple outgoing edges per node

```go
graph := &workflow.Graph{
    WorkflowName: "review_pipeline",
    Nodes: map[string]workflow.Node{
        "analyze":   analyzerNode,
        "critical":  criticalNode,
        "minor":     minorNode,
        "approve":   approveNode,
    },
    Edges: []workflow.Edge{
        {From: "analyze", To: "critical", Condition: hasCriticalIssues},
        {From: "analyze", To: "minor",    Condition: hasMinorIssues},
        {From: "analyze", To: "approve",  Condition: isClean},
    },
}
```

---

## Workflow Context

```go
type Context struct {
    StepLog    []LogEntry
    Step       int
    ResumeData any  // For interrupt/resume
}

type LogEntry struct {
    Step           int
    Node           string
    Input          any
    Output         any
    Error          string
    Interrupted    bool
    InterruptValue any
}
```

The `StepLog` enables idempotent resume: if a node already appears as successful in the log, its previous output is returned verbatim.

---

## Interrupt and Resume

Nodes can interrupt workflow execution to request external input:

```go
func approvalNode(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
    if wctx.ResumeData == nil {
        // First run: request human approval
        workflow.Interrupt(wctx, map[string]any{
            "action": "await_approval",
            "data":   input,
        })
        return nil, workflow.ErrInterrupted
    }
    // Resumed: process approval result
    approval := wctx.ResumeData.(map[string]any)
    if approval["approved"].(bool) {
        return "approved", nil
    }
    return "rejected", nil
}
```

Interrupted steps are NOT skipped on resume — the node re-runs and consumes `ResumeData`.

---

## devkit Integration

### AgentNode

`devkit` wraps the Agent as a workflow Node:

```go
// Create node with dedicated tape
node := kit.AsAgentNodeWithTape("writer", "wf-writer")
node.SystemPrompt = "You are a technical writer. Be concise."

// Or reuse the default tape
node := kit.AsAgentNode("writer")
```

### RunWorkflow

```go
result, err := kit.RunWorkflow(ctx, runner, input)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Completed in %d steps\n", result.Steps)
fmt.Println(result.Output)
```

---

## Checkpointing

Workflows support checkpoint/resume for long-running processes:

```go
// Save checkpoint
checkpoint := wctx.StepLog

// Resume later
wctx := &workflow.Context{StepLog: checkpoint}
result, _ := runner.Run(ctx, wctx, input)
```

---

## Workflow DSL (Declarative)

The `workflow/dsl` and `workflow/compiler` packages support declarative workflow definitions:

```yaml
# workflow.yaml
name: content_pipeline
steps:
  - name: brainstorm
    type: agent
    prompt: "Generate 3 ideas for {{.topic}}"
    tape: wf-brain
  - name: draft
    type: agent
    prompt: "Draft content from: {{.previous}}"
    tape: wf-draft
  - name: review
    type: agent
    prompt: "Review and polish: {{.previous}}"
    tape: wf-review
```

Compile and run:

```go
dsl := workflow.ParseDSL(data)
compiled := workflow.Compile(dsl)
result, _ := compiled.Run(ctx, nil, map[string]any{"topic": "AI"})
```

See `workflow/compiler/` and `workflow/dsl/` for the full DSL schema.
