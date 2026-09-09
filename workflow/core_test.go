package workflow

import (
	"context"
	"testing"
)

func TestSequentialAndParallelCore(t *testing.T) {
	seq := &Sequential{WorkflowName: "seq", Nodes: []Node{
		NodeFunc{N: "a", F: func(_ context.Context, _ *Context, _ any) (any, error) { return "a", nil }},
		NodeFunc{N: "b", F: func(_ context.Context, _ *Context, in any) (any, error) { return in.(string) + "b", nil }},
	}}
	out, err := seq.Run(context.Background(), NewContext(), "")
	if err != nil || out.(*Result).Output != "ab" {
		t.Fatalf("sequential: out=%v err=%v", out, err)
	}

	par := &Parallel{WorkflowName: "par", Nodes: []Node{
		NodeFunc{N: "left", F: func(_ context.Context, c *Context, _ any) (any, error) { c.SetState("left", true); return "l", nil }},
		NodeFunc{N: "right", F: func(_ context.Context, c *Context, _ any) (any, error) { c.SetState("right", true); return "r", nil }},
	}}
	wctx := NewContext()
	out, err = par.Run(context.Background(), wctx, nil)
	if err != nil || len(out.(*Result).Output.([]any)) != 2 {
		t.Fatalf("parallel: out=%v err=%v", out, err)
	}
	if wctx.State["left"] != true || wctx.State["right"] != true || len(wctx.StepLog) != 2 {
		t.Fatalf("parallel state/checkpoint: %#v %+v", wctx.State, wctx.StepLog)
	}
}

func TestInterruptResume(t *testing.T) {
	wctx := NewContext()
	n := NodeFunc{N: "approval", F: func(_ context.Context, c *Context, _ any) (any, error) { return Interrupt(c, "approve") }}
	seq := &Sequential{WorkflowName: "approval", Nodes: []Node{n}}
	if _, err := seq.Run(context.Background(), wctx, nil); !IsInterrupt(err) {
		t.Fatalf("expected interrupt, got %v", err)
	}
	wctx.ResumeData = "approved"
	out, err := seq.Run(context.Background(), wctx, nil)
	if err != nil || out.(*Result).Output != "approved" {
		t.Fatalf("resume: out=%v err=%v", out, err)
	}
}
