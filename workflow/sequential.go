package workflow

import (
	"context"
	"fmt"
	"iter"
)

// Sequential executes its sub-nodes one after another in the order provided.
// The output of each node becomes the input to the next.  State is preserved
// across steps so nodes may also communicate through wctx.State.
type Sequential struct {
	WorkflowName string
	Nodes        []Node
}

// Name returns the workflow name; satisfies [workflow.Node].
func (s *Sequential) Name() string { return s.WorkflowName }

// Run executes the sequential workflow (synchronous facade).
func (s *Sequential) Run(ctx context.Context, wctx *Context, input any) (any, error) {
	return runEventsFacade(s, ctx, wctx, input)
}

// RunEvents emits an event stream as the workflow executes.
func (s *Sequential) RunEvents(ctx context.Context, wctx *Context, input any) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		if wctx == nil {
			wctx = NewContext()
		}

		if !yield(newEvent(EventTypeWorkflowStart, s.WorkflowName, "", wctx.Step), nil) {
			return
		}

		current := input
		for _, n := range s.Nodes {
			if err := ctx.Err(); err != nil {
				_ = yield(nil, fmt.Errorf("workflow %q: context cancelled: %w", s.WorkflowName, err))
				return
			}

			if skipped, out := resumeSkip(wctx, n); skipped {
				ev := newEvent(EventTypeNodeSkip, s.WorkflowName, n.Name(), wctx.Step)
				ev.Output = out
				if !yield(ev, nil) {
					return
				}
				current = out
				wctx.Step++
				continue
			}

			// NodeStart
			if !yield(newEvent(EventTypeNodeStart, s.WorkflowName, n.Name(), wctx.Step), nil) {
				return
			}

			// Execute
			out, err := runNodeWithSpan(ctx, wctx, wctx.Step, n, current)

			// NodeEnd
			endEv := newEvent(EventTypeNodeEnd, s.WorkflowName, n.Name(), wctx.Step)
			endEv.Output = out
			if err != nil {
				endEv.Error = err.Error()
			}
			if !yield(endEv, nil) {
				return
			}

			if err != nil {
				final := &Result{Output: current, Error: err, Steps: wctx.Step}
				if ie, ok := err.(*InterruptError); ok {
					_ = yield(newEvent(EventTypeInterrupt, s.WorkflowName, n.Name(), wctx.Step), nil)
					final.Output = ie.Value
				}
				endEv := newEvent(EventTypeWorkflowEnd, s.WorkflowName, "", wctx.Step)
				endEv.Result = final
				_ = yield(endEv, nil)
				return
			}

			current = out
		}

		final := &Result{Output: current, Steps: wctx.Step}
		endEv := newEvent(EventTypeWorkflowEnd, s.WorkflowName, "", wctx.Step)
		endEv.Result = final
		_ = yield(endEv, nil)
	}
}
