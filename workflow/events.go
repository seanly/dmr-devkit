package workflow

import (
	"context"
	"fmt"
	"iter"
	"time"
)

// EventType describes what happened during workflow execution.
type EventType string

const (
	EventTypeWorkflowStart EventType = "workflow_start"
	EventTypeWorkflowEnd   EventType = "workflow_end"
	EventTypeNodeStart     EventType = "node_start"
	EventTypeNodeEnd       EventType = "node_end"
	EventTypeNodeSkip      EventType = "node_skip" // resumed step, skipped
	EventTypeStateDelta    EventType = "state_delta"
	EventTypeInterrupt     EventType = "interrupt"
)

// Event is an atomic occurrence during workflow execution.
type Event struct {
	Type       EventType      `json:"type"`
	Workflow   string         `json:"workflow"`
	Node       string         `json:"node,omitempty"`
	Step       int            `json:"step"`
	Input      any            `json:"input,omitempty"`
	Output     any            `json:"output,omitempty"`
	Error      string         `json:"error,omitempty"`
	StateDelta map[string]any `json:"state_delta,omitempty"`
	Result     *Result        `json:"-"` // set on WorkflowEnd
	Timestamp  time.Time      `json:"timestamp"`
}

// EventStream is a Runner that can emit execution events.
type EventStream interface {
	Runner
	RunEvents(ctx context.Context, wctx *Context, input any) iter.Seq2[*Event, error]
}

// eventStreamNode is a Node that can emit its own events.
type eventStreamNode interface {
	Node
	RunNodeEvents(ctx context.Context, wctx *Context, input any) iter.Seq2[*Event, error]
}

// resumeSkip checks whether a node was already completed successfully in StepLog.
// If so it returns true and the cached output.
func resumeSkip(wctx *Context, n Node) (skipped bool, output any) {
	for _, e := range wctx.StepLog {
		if e.Step == wctx.Step && e.Node == n.Name() && e.Error == "" && !e.Interrupted {
			return true, e.Output
		}
	}
	return false, nil
}

// runEventsFacade consumes an EventStream and returns the final *Result.
// It is used by Run() facades so existing synchronous callers keep working.
func runEventsFacade(es EventStream, ctx context.Context, wctx *Context, input any) (*Result, error) {
	var result *Result
	for ev, err := range es.RunEvents(ctx, wctx, input) {
		if err != nil {
			if result == nil {
				result = &Result{Error: err}
			} else {
				result.Error = err
			}
			return result, err
		}
		if ev.Type == EventTypeWorkflowEnd && ev.Result != nil {
			result = ev.Result
		}
	}
	if result == nil {
		return nil, fmt.Errorf("workflow: no end event produced")
	}
	if result.Error != nil {
		return result, result.Error
	}
	return result, nil
}

// newEvent creates an Event with the current timestamp.
func newEvent(et EventType, workflow, node string, step int) *Event {
	return &Event{
		Type:      et,
		Workflow:  workflow,
		Node:      node,
		Step:      step,
		Timestamp: time.Now(),
	}
}
