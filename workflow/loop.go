package workflow

import (
	"context"
	"fmt"
	"iter"
)

// Loop repeatedly executes its sub-nodes in sequence until either MaxIter is
// reached or Condition returns false after a full round.
//
// Each sub-node execution is logged with a monotonically increasing step number
// (via wctx.Step), which means Loop supports fine-grained resume: if execution
// is interrupted mid-loop, re-running the Loop will skip already-successful
// steps recorded in StepLog.
//
// Example – iterative refinement (max 5 rounds, stop when state signals done):
//
//	loop := &Loop{
//	    WorkflowName: "refine",
//	    MaxIter:      5,
//	    Nodes:        []Node{writer, critic},
//	    Condition: func(wctx *workflow.Context) bool {
//		    done, _ := wctx.GetState("done").(bool)
//		    return !done   // continue while not done
//	    },
//	}
type Loop struct {
	WorkflowName string
	Nodes        []Node
	// MaxIter is the hard upper bound on iterations. Must be > 0.
	MaxIter int
	// Condition is evaluated after each full round. Return true to continue
	// looping, false to break early. If nil, Loop runs exactly MaxIter rounds.
	Condition func(wctx *Context) bool
}

// Name returns the workflow name; satisfies [workflow.Node].
func (l *Loop) Name() string { return l.WorkflowName }

// Run executes the loop workflow (synchronous facade).
func (l *Loop) Run(ctx context.Context, wctx *Context, input any) (any, error) {
	return runEventsFacade(l, ctx, wctx, input)
}

// RunEvents emits an event stream as the loop executes.
func (l *Loop) RunEvents(ctx context.Context, wctx *Context, input any) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		if wctx == nil {
			wctx = NewContext()
		}
		if l.MaxIter <= 0 {
			_ = yield(nil, fmt.Errorf("workflow %q: MaxIter must be > 0", l.WorkflowName))
			return
		}
		if len(l.Nodes) == 0 {
			final := &Result{Output: input, Steps: 0}
			ev := newEvent(EventTypeWorkflowEnd, l.WorkflowName, "", wctx.Step)
			ev.Result = final
			_ = yield(ev, nil)
			return
		}

		if !yield(newEvent(EventTypeWorkflowStart, l.WorkflowName, "", wctx.Step), nil) {
			return
		}

		current := input

		for i := 0; i < l.MaxIter; i++ {
			if err := ctx.Err(); err != nil {
				_ = yield(nil, fmt.Errorf("workflow %q: context cancelled at iteration %d: %w", l.WorkflowName, i, err))
				return
			}

			for _, n := range l.Nodes {
				if skipped, out := resumeSkip(wctx, n); skipped {
					ev := newEvent(EventTypeNodeSkip, l.WorkflowName, n.Name(), wctx.Step)
					ev.Output = out
					if !yield(ev, nil) {
						return
					}
					current = out
					wctx.Step++
					continue
				}

				if !yield(newEvent(EventTypeNodeStart, l.WorkflowName, n.Name(), wctx.Step), nil) {
					return
				}

				out, err := runNode(ctx, wctx, wctx.Step, n, current)

				endEv := newEvent(EventTypeNodeEnd, l.WorkflowName, n.Name(), wctx.Step)
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
						_ = yield(newEvent(EventTypeInterrupt, l.WorkflowName, n.Name(), wctx.Step), nil)
						final.Output = ie.Value
					}
					endEv := newEvent(EventTypeWorkflowEnd, l.WorkflowName, "", wctx.Step)
					endEv.Result = final
					_ = yield(endEv, nil)
					return
				}

				current = out
			}

			// Evaluate termination condition after a full round.
			if l.Condition != nil && !l.Condition(wctx) {
				break
			}
		}

		final := &Result{Output: current, Steps: wctx.Step}
		endEv := newEvent(EventTypeWorkflowEnd, l.WorkflowName, "", wctx.Step)
		endEv.Result = final
		_ = yield(endEv, nil)
	}
}
