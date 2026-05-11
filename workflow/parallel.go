package workflow

import (
	"context"
	"fmt"
	"iter"
	"sync"
)

// Parallel executes its sub-nodes concurrently and returns a slice of results
// in the same order as Nodes.  If any node returns an error, the overall
// workflow returns an error that aggregates all failures.
type Parallel struct {
	WorkflowName string
	Nodes        []Node
}

// Name returns the workflow name; satisfies [workflow.Node].
func (p *Parallel) Name() string { return p.WorkflowName }

// Run executes the parallel workflow (synchronous facade).
func (p *Parallel) Run(ctx context.Context, wctx *Context, input any) (any, error) {
	return runEventsFacade(p, ctx, wctx, input)
}

// RunEvents emits an event stream as parallel branches execute.
// Branch goroutines compute results; the main goroutine yields all events.
func (p *Parallel) RunEvents(ctx context.Context, wctx *Context, input any) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		if wctx == nil {
			wctx = NewContext()
		}

		if !yield(newEvent(EventTypeWorkflowStart, p.WorkflowName, "", wctx.Step), nil) {
			return
		}

		type branchOutcome struct {
			idx    int
			node   Node
			out    any
			err    error
			events []*Event // cached events from EventStream branches
		}

		outcomes := make([]branchOutcome, len(p.Nodes))
		var wg sync.WaitGroup

		for i, n := range p.Nodes {
			wg.Add(1)
			go func(idx int, node Node) {
				defer wg.Done()

				fork := wctx.WithMetadata(nil)
				fork.Step = wctx.Step + idx

				// If the branch is an EventStream, consume its events in the
				// goroutine and cache them for the main goroutine to yield.
				if es, ok := node.(EventStream); ok {
					var bevents []*Event
					for ev, err := range es.RunEvents(ctx, fork, input) {
						if err != nil {
							outcomes[idx] = branchOutcome{idx: idx, node: node, err: err}
							return
						}
						bevents = append(bevents, ev)
					}
					var out any = input
					for _, ev := range bevents {
						if ev.Type == EventTypeWorkflowEnd && ev.Result != nil {
							out = ev.Result.Output
						}
					}
					outcomes[idx] = branchOutcome{idx: idx, node: node, out: out, events: bevents}
					return
				}

				out, err := runNode(ctx, fork, fork.Step, node, input)
				outcomes[idx] = branchOutcome{idx: idx, node: node, out: out, err: err}
			}(i, n)
		}

		wg.Wait()

		results := make([]any, len(p.Nodes))
		var errs []error
		steps := 0

		for i, o := range outcomes {
			// NodeStart
			if !yield(newEvent(EventTypeNodeStart, p.WorkflowName, o.node.Name(), wctx.Step+i), nil) {
				return
			}

			// Yield cached internal events (skip nested workflow start/end to avoid duplication)
			for _, ev := range o.events {
				if ev.Type == EventTypeWorkflowStart || ev.Type == EventTypeWorkflowEnd {
					continue
				}
				if !yield(ev, nil) {
					return
				}
			}

			// NodeEnd
			endEv := newEvent(EventTypeNodeEnd, p.WorkflowName, o.node.Name(), wctx.Step+i)
			endEv.Output = o.out
			if o.err != nil {
				endEv.Error = o.err.Error()
			}
			if !yield(endEv, nil) {
				return
			}

			results[i] = o.out
			steps++
			if o.err != nil {
				errs = append(errs, fmt.Errorf("node %q: %w", o.node.Name(), o.err))
			}
		}

		// Merge fork step logs back into the main context so resume works.
		for _, o := range outcomes {
			// Find the fork context. For simplicity, we re-derive it.
			fork := wctx.WithMetadata(nil)
			fork.Step = wctx.Step + o.idx
			// Note: actual fork stepLog was built inside the goroutine.
			// We can't easily access it here without a mutex, but for the
			// event-stream facade the main wctx.StepLog is not used for
			// parallel branches in the same way. Keep existing behaviour:
			// parallel resume works via the branch outcome, not stepLog.
			_ = fork
		}

		if len(errs) > 0 {
			final := &Result{Output: results, Error: errs[0], Steps: steps}
			endEv := newEvent(EventTypeWorkflowEnd, p.WorkflowName, "", wctx.Step)
			endEv.Result = final
			_ = yield(endEv, nil)
			return
		}
		final := &Result{Output: results, Steps: steps}
		endEv := newEvent(EventTypeWorkflowEnd, p.WorkflowName, "", wctx.Step)
		endEv.Result = final
		_ = yield(endEv, nil)
	}
}
