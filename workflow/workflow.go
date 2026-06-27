// Package workflow provides deterministic orchestration of agent tasks using
// sequential, parallel, and graph-based execution patterns.
//
// It is designed to work alongside pkg/devkit without introducing circular
// dependencies. The core types depend only on the standard library.
package workflow

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/observe"
)

// Node is the basic executable unit in a workflow.
type Node interface {
	Name() string
	Run(ctx context.Context, wctx *Context, input any) (any, error)
}

// NodeFunc is a convenience adapter to turn a plain function into a Node.
type NodeFunc struct {
	N string
	F func(ctx context.Context, wctx *Context, input any) (any, error)
}

func (n NodeFunc) Name() string { return n.N }

func (n NodeFunc) Run(ctx context.Context, wctx *Context, input any) (any, error) {
	return n.F(ctx, wctx, input)
}

// Result is the standardized output of any workflow run.
type Result struct {
	Output any
	Error  error
	Steps  int // number of nodes executed
}

// Runner executes a workflow and returns a *Result wrapped as any.
// Sequential, Parallel, and Graph all satisfy this interface and can also
// be used as Nodes inside larger workflows.
type Runner interface {
	Run(ctx context.Context, wctx *Context, input any) (any, error)
}

// LogEntry records a single step execution for checkpoint/resume.
type LogEntry struct {
	Step           int
	Node           string
	Input          any
	Output         any
	Error          string // empty when success
	Interrupted    bool   // true when the node called workflow.Interrupt
	InterruptValue any    // payload surfaced to the external caller
}

// log records a step into the context's step log.
func log(wctx *Context, step int, name string, input, output any, err error) {
	entry := LogEntry{Step: step, Node: name, Input: input, Output: output}
	if err != nil {
		entry.Error = err.Error()
	}
	wctx.StepLog = append(wctx.StepLog, entry)
	wctx.Step++
}

// runNode executes a single node with standardised logging and recovery.
// If the node already appears as successful in StepLog, its previous output
// is returned verbatim (idempotent resume).
// Interrupted steps are NOT skipped so that the node can re-run and consume
// ResumeData via workflow.Interrupt.
func runNode(ctx context.Context, wctx *Context, step int, n Node, input any) (any, error) {
	// Resume: skip already-successful executions. Iterate backward so the most
	// recent entry for a given (step, node) pair takes precedence.
	for i := len(wctx.StepLog) - 1; i >= 0; i-- {
		e := wctx.StepLog[i]
		if e.Step == step && e.Node == n.Name() && e.Error == "" && !e.Interrupted {
			wctx.Step = step + 1
			return e.Output, nil
		}
	}

	out, err := n.Run(ctx, wctx, input)
	// Unwrap nested workflow Results so downstream nodes receive raw outputs.
	if r, ok := out.(*Result); ok {
		out = r.Output
	}

	// Record interrupt in StepLog so it is observable and step numbering stays consistent.
	if ie, ok := err.(*InterruptError); ok {
		wctx.StepLog = append(wctx.StepLog, LogEntry{
			Step:           step,
			Node:           n.Name(),
			Input:          input,
			Output:         out,
			Interrupted:    true,
			InterruptValue: ie.Value,
		})
		wctx.Step = step + 1
		return out, err
	}

	log(wctx, step, n.Name(), input, out, err)
	return out, err
}

// runNodeNamed is a helper for graph execution that looks up a node by name.
func runNodeNamed(ctx context.Context, wctx *Context, step int, nodes map[string]Node, name string, input any) (any, error) {
	n, ok := nodes[name]
	if !ok {
		return nil, fmt.Errorf("workflow: node %q not found", name)
	}
	return runNodeWithSpan(ctx, wctx, step, n, input)
}

// runNodeWithSpan executes a node, wrapping it in an observe span when a tracer
// is present in the context.
func runNodeWithSpan(ctx context.Context, wctx *Context, step int, n Node, input any) (any, error) {
	if tr := observe.TracerFromContext(ctx); tr != nil {
		ctx, finish := tr.StartNode(ctx, n.Name(), step)
		out, err := runNode(ctx, wctx, step, n, input)
		finish(err)
		return out, err
	}
	return runNode(ctx, wctx, step, n, input)
}
