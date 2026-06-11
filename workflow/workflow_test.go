package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countNode is a test node that appends its name to a string.
type countNode struct {
	name string
}

func (n countNode) Name() string { return n.name }

func (n countNode) Run(_ context.Context, _ *Context, input any) (any, error) {
	prev, _ := input.(string)
	if prev == "" {
		return n.name, nil
	}
	return prev + ">" + n.name, nil
}

// failNode is a test node that always errors.
type failNode struct{ name string }

func (n failNode) Name() string { return n.name }

func (n failNode) Run(_ context.Context, _ *Context, _ any) (any, error) {
	return nil, fmt.Errorf("node %s failed", n.name)
}

// sleepNode sleeps for a duration and records whether it ran.
type sleepNode struct {
	name     string
	duration time.Duration
	ran      *atomic.Bool
}

func (n sleepNode) Name() string { return n.name }

func (n sleepNode) Run(_ context.Context, _ *Context, _ any) (any, error) {
	time.Sleep(n.duration)
	n.ran.Store(true)
	return n.name, nil
}

// stateNode writes and reads from workflow context state.
type stateNode struct {
	name string
	key  string
	val  string
}

func (n stateNode) Name() string { return n.name }

func (n stateNode) Run(_ context.Context, wctx *Context, input any) (any, error) {
	wctx.SetState(n.key, n.val)
	return fmt.Sprintf("%s:%s", input, n.val), nil
}

// --- Sequential Tests ---

func TestSequential_Basic(t *testing.T) {
	t.Parallel()
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes:        []Node{countNode{"a"}, countNode{"b"}, countNode{"c"}},
	}
	out, err := seq.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	want := "a>b>c"
	if got, _ := res.Output.(string); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if res.Steps != 3 {
		t.Fatalf("steps = %d, want 3", res.Steps)
	}
}

func TestSequential_PassesState(t *testing.T) {
	t.Parallel()
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes:        []Node{stateNode{"n1", "k1", "v1"}, stateNode{"n2", "k2", "v2"}},
	}
	wctx := NewContext()
	out, err := seq.Run(context.Background(), wctx, "start")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	want := "start:v1:v2"
	if got, _ := res.Output.(string); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if wctx.State["k1"] != "v1" || wctx.State["k2"] != "v2" {
		t.Fatalf("state not propagated: %v", wctx.State)
	}
}

func TestSequential_ErrorStopsExecution(t *testing.T) {
	t.Parallel()
	var ran atomic.Bool
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes: []Node{
			countNode{"a"},
			failNode{"b"},
			NodeFunc{
				N: "c",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					ran.Store(true)
					return "c", nil
				},
			},
		},
	}
	out, err := seq.Run(context.Background(), NewContext(), "")
	res := out.(*Result)
	if err == nil {
		t.Fatal("expected error")
	}
	if ran.Load() {
		t.Fatal("node c should not have run after failure")
	}
	if res.Steps != 2 {
		t.Fatalf("steps = %d, want 2", res.Steps)
	}
}

func TestSequential_Resume(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes: []Node{
			NodeFunc{
				N: "a",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					calls.Add(1)
					return "a-out", nil
				},
			},
			NodeFunc{
				N: "b",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					calls.Add(1)
					return "b-out", nil
				},
			},
		},
	}
	wctx := NewContext()
	// Pre-populate step log so that node "a" is skipped.
	wctx.StepLog = append(wctx.StepLog, LogEntry{Step: 0, Node: "a", Output: "a-out"})
	out, err := seq.Run(context.Background(), wctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if calls.Load() != 1 {
		t.Fatalf("node b should run once, calls = %d", calls.Load())
	}
	if res.Output != "b-out" {
		t.Fatalf("output = %v, want b-out", res.Output)
	}
}

// --- Parallel Tests ---

func TestParallel_Basic(t *testing.T) {
	t.Parallel()
	par := &Parallel{
		WorkflowName: "par",
		Nodes:        []Node{countNode{"a"}, countNode{"b"}, countNode{"c"}},
	}
	out, err := par.Run(context.Background(), NewContext(), "in")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	outs, ok := res.Output.([]any)
	if !ok || len(outs) != 3 {
		t.Fatalf("output = %v, want 3-element slice", res.Output)
	}
	// Order preserved despite parallel execution.
	if outs[0] != "in>a" || outs[1] != "in>b" || outs[2] != "in>c" {
		t.Fatalf("outputs = %v", outs)
	}
}

func TestParallel_ActuallyParallel(t *testing.T) {
	t.Parallel()
	var ran1, ran2 atomic.Bool
	par := &Parallel{
		WorkflowName: "par",
		Nodes: []Node{
			sleepNode{"slow", 100 * time.Millisecond, &ran1},
			sleepNode{"fast", 10 * time.Millisecond, &ran2},
		},
	}
	start := time.Now()
	_, err := par.Run(context.Background(), NewContext(), nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran1.Load() || !ran2.Load() {
		t.Fatal("both nodes should have run")
	}
	// Should complete in ~100ms, not ~110ms.
	// Threshold at 250ms to tolerate CI/scheduling variance.
	if elapsed > 250*time.Millisecond {
		t.Fatalf("too slow: %v (expected parallel execution)", elapsed)
	}
}

func TestParallel_ErrorAggregation(t *testing.T) {
	t.Parallel()
	par := &Parallel{
		WorkflowName: "par",
		Nodes:        []Node{countNode{"a"}, failNode{"b"}, countNode{"c"}},
	}
	out, err := par.Run(context.Background(), NewContext(), nil)
	res := out.(*Result)
	if err == nil {
		t.Fatal("expected error")
	}
	// Even with an error, the other nodes should have produced outputs.
	outs, _ := res.Output.([]any)
	if len(outs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(outs))
	}
}

// --- Graph Tests ---

func TestGraph_SequentialChain(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("a", countNode{"a"})
	g.AddNode("b", countNode{"b"})
	g.AddNode("c", countNode{"c"})
	g.AddEdge("START", "a")
	g.AddEdge("a", "b")
	g.AddEdge("b", "c")

	out, err := g.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if want := "a>b>c"; res.Output != want {
		t.Fatalf("output = %q, want %q", res.Output, want)
	}
}

func TestGraph_Router(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("classify", NodeFunc{
		N: "classify",
		F: func(_ context.Context, _ *Context, input any) (any, error) {
			s, _ := input.(string)
			return strings.ToUpper(s), nil
		},
	})
	g.AddNode("bug_handler", countNode{"BUG"})
	g.AddNode("feature_handler", countNode{"FEAT"})

	g.AddEdge("START", "classify")
	g.AddEdge("classify", "route")
	g.AddRouter("route", func(_ context.Context, _ *Context, input any) (string, any, error) {
		s, _ := input.(string)
		if s == "BUG" {
			return "bug", "bug-payload", nil
		}
		return "feature", "feature-payload", nil
	}, map[string]string{
		"bug":     "bug_handler",
		"feature": "feature_handler",
	})

	out, err := g.Run(context.Background(), NewContext(), "bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if want := "bug-payload>BUG"; res.Output != want {
		t.Fatalf("output = %q, want %q", res.Output, want)
	}
}

func TestGraph_ParallelStart(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("a", countNode{"a"})
	g.AddNode("b", countNode{"b"})
	g.AddEdge("START", "a")
	g.AddEdge("START", "b")

	out, err := g.Run(context.Background(), NewContext(), "in")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	outs, ok := res.Output.([]any)
	if !ok || len(outs) != 2 {
		t.Fatalf("output = %v, want 2-element slice", res.Output)
	}
}

func TestGraph_MissingNode(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddEdge("START", "nonexistent")
	_, err := g.Run(context.Background(), NewContext(), nil)
	if err == nil {
		t.Fatal("expected error for missing node")
	}
}

// --- Context Tests ---

func TestContext_WithMetadata(t *testing.T) {
	c := NewContext()
	c.SetState("x", 1)
	c.Metadata["m1"] = "v1"

	c2 := c.WithMetadata(map[string]any{"m2": "v2"})
	if c2.State["x"] != 1 {
		t.Fatal("state not copied")
	}
	if c2.Metadata["m1"] != "v1" || c2.Metadata["m2"] != "v2" {
		t.Fatal("metadata not merged")
	}
	// Original should be untouched.
	if _, ok := c.Metadata["m2"]; ok {
		t.Fatal("original metadata mutated")
	}
}

func TestContext_GetSetState(t *testing.T) {
	c := NewContext()
	if _, ok := c.GetState("missing"); ok {
		t.Fatal("expected missing key to be absent")
	}
	c.SetState("k", "v")
	if v, ok := c.GetState("k"); !ok || v != "v" {
		t.Fatalf("state mismatch: %v", v)
	}
}

// --- Integration-style test ---

func TestSequentialThenParallel(t *testing.T) {
	t.Parallel()
	// First stage: sequential pre-processing
	pre := &Sequential{
		WorkflowName: "pre",
		Nodes:        []Node{countNode{"prep"}},
	}
	// Second stage: parallel processing
	par := &Parallel{
		WorkflowName: "par",
		Nodes:        []Node{countNode{"a"}, countNode{"b"}},
	}
	// Combine them into a larger sequential workflow.
	combined := &Sequential{
		WorkflowName: "combined",
		Nodes: []Node{
			pre,
			par,
		},
	}

	out, err := combined.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	// pre outputs "prep", then par receives "prep" as input for both branches.
	outs, ok := res.Output.([]any)
	if !ok || len(outs) != 2 {
		t.Fatalf("output = %v", res.Output)
	}
	if outs[0] != "prep>a" || outs[1] != "prep>b" {
		t.Fatalf("outputs = %v", outs)
	}
}

func TestGraph_NestedWorkflow(t *testing.T) {
	t.Parallel()
	inner := &Sequential{
		WorkflowName: "inner",
		Nodes:        []Node{countNode{"inner-a"}, countNode{"inner-b"}},
	}
	g := &Graph{Name: "outer"}
	g.AddNode("pre", countNode{"pre"})
	g.AddNode("inner", inner)
	g.AddNode("post", countNode{"post"})
	g.AddEdge("START", "pre")
	g.AddEdge("pre", "inner")
	g.AddEdge("inner", "post")

	out, err := g.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	// Sequential workflow as a node passes its output forward.
	want := "pre>inner-a>inner-b>post"
	if res.Output != want {
		t.Fatalf("output = %q, want %q", res.Output, want)
	}
}

// --- Conditional Edges + Pre-built Routers ---

func TestGraph_ConditionalEdges(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("source", NodeFunc{
		N: "source",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "hello", nil },
	})
	g.AddNode("target_a", NodeFunc{
		N: "target_a",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "A", nil },
	})
	g.AddNode("target_b", NodeFunc{
		N: "target_b",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "B", nil },
	})

	g.AddEdge("START", "source")
	g.AddConditionalEdges("source",
		func(_ context.Context, _ *Context, input any) (string, any, error) {
			s, _ := input.(string)
			if s == "hello" {
				return "a", s, nil
			}
			return "b", s, nil
		},
		map[string]string{
			"a": "target_a",
			"b": "target_b",
		},
	)

	out, err := g.Run(context.Background(), NewContext(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if res.Output != "A" {
		t.Fatalf("output = %q, want A", res.Output)
	}
}

func TestGraph_ExactMatchRouter(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("src", NodeFunc{
		N: "src",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "BUG", nil },
	})
	g.AddNode("bug", NodeFunc{
		N: "bug",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "BUG-HANDLER", nil },
	})
	g.AddNode("feat", NodeFunc{
		N: "feat",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "FEAT-HANDLER", nil },
	})

	g.AddEdge("START", "src")
	g.AddConditionalEdges("src",
		ExactMatchRouter(map[string]string{
			"bug":  "BUG",
			"feat": "FEATURE",
		}),
		map[string]string{
			"bug":  "bug",
			"feat": "feat",
		},
	)

	out, err := g.Run(context.Background(), NewContext(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if res.Output != "BUG-HANDLER" {
		t.Fatalf("output = %q, want BUG-HANDLER", res.Output)
	}
}

func TestGraph_ContainsRouter(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("src", NodeFunc{
		N: "src",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "this is CRITICAL alert", nil },
	})
	g.AddNode("crit", NodeFunc{
		N: "crit",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "CRIT", nil },
	})
	g.AddNode("warn", NodeFunc{
		N: "warn",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "WARN", nil },
	})

	g.AddEdge("START", "src")
	g.AddConditionalEdges("src",
		ContainsRouter(map[string]string{
			"critical": "CRITICAL",
			"warning":  "WARNING",
		}),
		map[string]string{
			"critical": "crit",
			"warning":  "warn",
		},
	)

	out, err := g.Run(context.Background(), NewContext(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if res.Output != "CRIT" {
		t.Fatalf("output = %q, want CRIT", res.Output)
	}
}

func TestGraph_PrefixRouter(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("src", NodeFunc{
		N: "src",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "ERROR: disk full", nil },
	})
	g.AddNode("err", NodeFunc{
		N: "err",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "ERR", nil },
	})
	g.AddNode("info", NodeFunc{
		N: "info",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "INFO", nil },
	})

	g.AddEdge("START", "src")
	g.AddConditionalEdges("src",
		PrefixRouter(map[string]string{
			"error": "ERROR:",
		}),
		map[string]string{
			"error": "err",
			"info":  "info",
		},
	)

	out, err := g.Run(context.Background(), NewContext(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if res.Output != "ERR" {
		t.Fatalf("output = %q, want ERR", res.Output)
	}
}

func TestGraph_DefaultRouter(t *testing.T) {
	t.Parallel()
	g := &Graph{Name: "g"}
	g.AddNode("src", NodeFunc{
		N: "src",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "UNKNOWN", nil },
	})
	g.AddNode("bug", NodeFunc{
		N: "bug",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "BUG", nil },
	})
	g.AddNode("fallback", NodeFunc{
		N: "fallback",
		F: func(_ context.Context, _ *Context, _ any) (any, error) { return "FALLBACK", nil },
	})

	g.AddEdge("START", "src")
	g.AddConditionalEdges("src",
		Default(
			ExactMatchRouter(map[string]string{
				"bug": "BUG",
			}),
			"fallback",
		),
		map[string]string{
			"bug":      "bug",
			"fallback": "fallback",
		},
	)

	out, err := g.Run(context.Background(), NewContext(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if res.Output != "FALLBACK" {
		t.Fatalf("output = %q, want FALLBACK", res.Output)
	}
}

// --- Loop Tests ---

func TestLoop_Basic(t *testing.T) {
	t.Parallel()
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      3,
		Nodes:        []Node{countNode{"a"}, countNode{"b"}},
	}
	out, err := loop.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	// Each round prepends the chain. After 3 rounds: a>b>a>b>a>b
	want := "a>b>a>b>a>b"
	if got, _ := res.Output.(string); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if res.Steps != 6 {
		t.Fatalf("steps = %d, want 6", res.Steps)
	}
}

func TestLoop_ConditionEarlyBreak(t *testing.T) {
	t.Parallel()
	rounds := 0
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      10,
		Nodes:        []Node{countNode{"a"}},
		Condition: func(wctx *Context) bool {
			rounds++
			// Stop after 2 full rounds.
			return rounds < 2
		},
	}
	out, err := loop.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	// Two rounds: a>a
	want := "a>a"
	if got, _ := res.Output.(string); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if res.Steps != 2 {
		t.Fatalf("steps = %d, want 2", res.Steps)
	}
}

func TestLoop_ConditionViaState(t *testing.T) {
	t.Parallel()
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      10,
		Nodes: []Node{
			NodeFunc{
				N: "inc",
				F: func(_ context.Context, wctx *Context, input any) (any, error) {
					v, _ := wctx.State["counter"].(int)
					wctx.SetState("counter", v+1)
					return v + 1, nil
				},
			},
		},
		Condition: func(wctx *Context) bool {
			v, _ := wctx.State["counter"].(int)
			return v < 3 // continue while counter < 3
		},
	}
	out, err := loop.Run(context.Background(), NewContext(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	// counter goes 1,2,3. After round 3 counter==3, condition false, break.
	// But the third round already ran, so output is 3.
	if res.Output != 3 {
		t.Fatalf("output = %v, want 3", res.Output)
	}
	if res.Steps != 3 {
		t.Fatalf("steps = %d, want 3", res.Steps)
	}
}

func TestLoop_ErrorStopsExecution(t *testing.T) {
	t.Parallel()
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      5,
		Nodes: []Node{
			countNode{"a"},
			failNode{"b"},
			countNode{"c"},
		},
	}
	out, err := loop.Run(context.Background(), NewContext(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	res := out.(*Result)
	// a succeeds, b fails on first iteration.
	if res.Steps != 2 {
		t.Fatalf("steps = %d, want 2", res.Steps)
	}
}

func TestLoop_ResumeMidIteration(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      3,
		Nodes: []Node{
			NodeFunc{
				N: "a",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					calls.Add(1)
					return "a-out", nil
				},
			},
			NodeFunc{
				N: "b",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					calls.Add(1)
					return "b-out", nil
				},
			},
		},
	}
	wctx := NewContext()
	// Pre-populate step log so that first two steps (iter 0: a, b) are skipped.
	wctx.StepLog = append(wctx.StepLog,
		LogEntry{Step: 0, Node: "a", Output: "a-out"},
		LogEntry{Step: 1, Node: "b", Output: "b-out"},
	)
	out, err := loop.Run(context.Background(), wctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	// Only iter 1 and iter 2 should execute (4 node calls).
	if calls.Load() != 4 {
		t.Fatalf("expected 4 node calls after resume, got %d", calls.Load())
	}
	// NodeFuncs return fixed strings, so final output is just the last b's result.
	want := "b-out"
	if got, _ := res.Output.(string); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestLoop_EmptyNodes(t *testing.T) {
	t.Parallel()
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      5,
		Nodes:        []Node{},
	}
	out, err := loop.Run(context.Background(), NewContext(), "in")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	if res.Output != "in" {
		t.Fatalf("output = %v, want in", res.Output)
	}
	if res.Steps != 0 {
		t.Fatalf("steps = %d, want 0", res.Steps)
	}
}

func TestLoop_InvalidMaxIter(t *testing.T) {
	t.Parallel()
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      0,
		Nodes:        []Node{countNode{"a"}},
	}
	_, err := loop.Run(context.Background(), NewContext(), "")
	if err == nil {
		t.Fatal("expected error for MaxIter <= 0")
	}
}

func TestLoop_NestedInSequential(t *testing.T) {
	t.Parallel()
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      2,
		Nodes:        []Node{countNode{"inner"}},
	}
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes:        []Node{countNode{"pre"}, loop, countNode{"post"}},
	}
	out, err := seq.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	// pre -> inner>inner -> post
	want := "pre>inner>inner>post"
	if got, _ := res.Output.(string); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestLoop_NestedInGraph(t *testing.T) {
	t.Parallel()
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      2,
		Nodes:        []Node{countNode{"inner"}},
	}
	g := &Graph{Name: "g"}
	g.AddNode("pre", countNode{"pre"})
	g.AddNode("loop", loop)
	g.AddNode("post", countNode{"post"})
	g.AddEdge("START", "pre")
	g.AddEdge("pre", "loop")
	g.AddEdge("loop", "post")

	out, err := g.Run(context.Background(), NewContext(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out.(*Result)
	want := "pre>inner>inner>post"
	if res.Output != want {
		t.Fatalf("output = %q, want %q", res.Output, want)
	}
}

// --- Interrupt Tests ---

func TestInterrupt_Basic(t *testing.T) {
	t.Parallel()
	node := NodeFunc{
		N: "ask",
		F: func(_ context.Context, wctx *Context, input any) (any, error) {
			decision, err := Interrupt(wctx, map[string]any{"draft": input})
			if err != nil {
				return nil, err
			}
			return decision, nil
		},
	}
	wctx := NewContext()
	_, err := node.Run(context.Background(), wctx, "hello")
	if err == nil {
		t.Fatal("expected interrupt error")
	}
	if !IsInterrupt(err) {
		t.Fatalf("expected IsInterrupt true, got %v", err)
	}
	ie := err.(*InterruptError)
	if ie.Value == nil {
		t.Fatal("expected interrupt value")
	}

	// Resume: node re-executes but Interrupt returns ResumeData immediately.
	wctx.ResumeData = "approved"
	wctx.Step = 0
	out, err := node.Run(context.Background(), wctx, "hello")
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	if out != "approved" {
		t.Fatalf("output = %v, want approved", out)
	}
}

func TestInterrupt_IsInterrupt(t *testing.T) {
	t.Parallel()
	if IsInterrupt(nil) {
		t.Fatal("IsInterrupt(nil) should be false")
	}
	if IsInterrupt(fmt.Errorf("plain")) {
		t.Fatal("IsInterrupt(plain) should be false")
	}
	if !IsInterrupt(&InterruptError{Value: 1}) {
		t.Fatal("IsInterrupt(InterruptError) should be true")
	}
	wrapped := fmt.Errorf("wrapped: %w", &InterruptError{Value: 2})
	if !IsInterrupt(wrapped) {
		t.Fatal("IsInterrupt(wrapped) should be true")
	}
}

func TestInterrupt_StepLogRecordsInterrupt(t *testing.T) {
	t.Parallel()
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes: []Node{
			NodeFunc{
				N: "ask",
				F: func(_ context.Context, wctx *Context, _ any) (any, error) {
					_, err := Interrupt(wctx, "payload")
					return nil, err
				},
			},
		},
	}
	wctx := NewContext()
	_, _ = seq.Run(context.Background(), wctx, "in")
	if len(wctx.StepLog) != 1 {
		t.Fatalf("steplog len = %d, want 1", len(wctx.StepLog))
	}
	e := wctx.StepLog[0]
	if !e.Interrupted {
		t.Fatal("expected Interrupted=true")
	}
	if e.InterruptValue != "payload" {
		t.Fatalf("InterruptValue = %v, want payload", e.InterruptValue)
	}
	if e.Step != 0 {
		t.Fatalf("step = %d, want 0", e.Step)
	}
	if e.Error != "" {
		t.Fatalf("Error should be empty for interrupt, got %q", e.Error)
	}
}

func TestInterrupt_NoResumeData(t *testing.T) {
	t.Parallel()
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes: []Node{
			NodeFunc{
				N: "ask",
				F: func(_ context.Context, wctx *Context, _ any) (any, error) {
					_, err := Interrupt(wctx, "payload")
					return nil, err
				},
			},
		},
	}
	wctx := NewContext()
	_, err := seq.Run(context.Background(), wctx, "")
	if !IsInterrupt(err) {
		t.Fatal("expected interrupt")
	}

	// Re-run from step 0 without ResumeData: should interrupt again.
	wctx.Step = 0
	_, err = seq.Run(context.Background(), wctx, "")
	if !IsInterrupt(err) {
		t.Fatalf("expected interrupt again, got %v", err)
	}
	if len(wctx.StepLog) != 2 {
		t.Fatalf("steplog len = %d, want 2", len(wctx.StepLog))
	}
}

func TestInterrupt_InSequential(t *testing.T) {
	t.Parallel()
	var calls []string
	seq := &Sequential{
		WorkflowName: "seq",
		Nodes: []Node{
			NodeFunc{
				N: "a",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					calls = append(calls, "a")
					return "a-out", nil
				},
			},
			NodeFunc{
				N: "b",
				F: func(_ context.Context, wctx *Context, input any) (any, error) {
					calls = append(calls, "b")
					decision, err := Interrupt(wctx, "payload")
					if err != nil {
						return nil, err
					}
					return decision, nil
				},
			},
			NodeFunc{
				N: "c",
				F: func(_ context.Context, _ *Context, input any) (any, error) {
					calls = append(calls, "c")
					return input.(string) + ">c", nil
				},
			},
		},
	}
	wctx := NewContext()
	_, err := seq.Run(context.Background(), wctx, "")
	if !IsInterrupt(err) {
		t.Fatalf("expected interrupt, got %v", err)
	}
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Fatalf("first-run calls = %v", calls)
	}

	// Resume: a is skipped, b consumes ResumeData, c runs.
	wctx.ResumeData = "approved"
	wctx.Step = 0
	calls = nil
	out, err := seq.Run(context.Background(), wctx, "")
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	res := out.(*Result)
	if res.Output != "approved>c" {
		t.Fatalf("output = %v, want approved>c", res.Output)
	}
	if len(calls) != 2 || calls[0] != "b" || calls[1] != "c" {
		t.Fatalf("resume calls = %v", calls)
	}
}

func TestInterrupt_InLoop(t *testing.T) {
	t.Parallel()
	var calls []string
	loop := &Loop{
		WorkflowName: "loop",
		MaxIter:      3,
		Nodes: []Node{
			NodeFunc{
				N: "a",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					calls = append(calls, "a")
					return "a-out", nil
				},
			},
			NodeFunc{
				N: "approval",
				F: func(_ context.Context, wctx *Context, input any) (any, error) {
					calls = append(calls, "approval")
					decision, err := Interrupt(wctx, "need-approval")
					if err != nil {
						return nil, err
					}
					return decision, nil
				},
			},
		},
	}
	wctx := NewContext()
	_, err := loop.Run(context.Background(), wctx, "")
	if !IsInterrupt(err) {
		t.Fatalf("expected interrupt, got %v", err)
	}
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "approval" {
		t.Fatalf("first-run calls = %v", calls)
	}

	// Resume: iter 0 a skipped, approval consumes ResumeData, then iter 1 runs and interrupts again.
	wctx.ResumeData = "ok"
	wctx.Step = 0
	calls = nil
	out, err := loop.Run(context.Background(), wctx, "")
	if !IsInterrupt(err) {
		t.Fatalf("expected interrupt on iter 1, got %v", err)
	}
	res := out.(*Result)
	// After resume: approval@1 resumed -> "ok", iter1 a@2 -> "a-out", approval@3 interrupts.
	if len(calls) != 3 || calls[0] != "approval" || calls[1] != "a" || calls[2] != "approval" {
		t.Fatalf("resume calls = %v", calls)
	}
	if res.Steps != 4 {
		t.Fatalf("steps = %d, want 4", res.Steps)
	}
}

func TestInterrupt_NestedWorkflow(t *testing.T) {
	t.Parallel()
	var calls []string
	inner := &Sequential{
		WorkflowName: "inner",
		Nodes: []Node{
			NodeFunc{
				N: "a",
				F: func(_ context.Context, _ *Context, _ any) (any, error) {
					calls = append(calls, "a")
					return "a-out", nil
				},
			},
			NodeFunc{
				N: "b",
				F: func(_ context.Context, wctx *Context, _ any) (any, error) {
					calls = append(calls, "b")
					decision, err := Interrupt(wctx, "inner-payload")
					if err != nil {
						return nil, err
					}
					return decision, nil
				},
			},
		},
	}
	g := &Graph{Name: "outer"}
	g.AddNode("pre", countNode{"pre"})
	g.AddNode("inner", inner)
	g.AddNode("post", NodeFunc{
		N: "post",
		F: func(_ context.Context, _ *Context, input any) (any, error) {
			calls = append(calls, "post")
			s, _ := input.(string)
			return s + ">post", nil
		},
	})
	g.AddEdge("START", "pre")
	g.AddEdge("pre", "inner")
	g.AddEdge("inner", "post")

	wctx := NewContext()
	_, err := g.Run(context.Background(), wctx, "")
	if !IsInterrupt(err) {
		t.Fatalf("expected interrupt, got %v", err)
	}
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Fatalf("first-run calls = %v", calls)
	}

	// Resume
	wctx.ResumeData = "continue"
	wctx.Step = 0
	calls = nil
	out, err := g.Run(context.Background(), wctx, "")
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	res := out.(*Result)
	want := "continue>post"
	if res.Output != want {
		t.Fatalf("output = %v, want %v", res.Output, want)
	}
	if len(calls) != 2 || calls[0] != "b" || calls[1] != "post" {
		t.Fatalf("resume calls = %v", calls)
	}
}
