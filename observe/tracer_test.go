package observe

import (
	"context"
	"errors"
	"testing"
)

func TestTracer_StartCreatesHierarchy(t *testing.T) {
	tr := NewTracer()
	ctx := WithTracer(context.Background(), tr)

	ctx, finishAgent := tr.StartAgent(ctx, "researcher", "coder")
	ctx, finishLLM := tr.StartLLMCall(ctx, "anthropic", "claude-sonnet-4-6")
	ctx, finishTool := tr.StartToolCall(ctx, "web_search", `{"q":"go"}`)
	finishTool("results", nil)
	finishLLM(42, nil)
	finishAgent(nil)

	spans := tr.Spans()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}

	agent := spans[0]
	llm := spans[1]
	tool := spans[2]

	if agent.Kind != SpanKindAgent {
		t.Fatalf("expected agent span, got %s", agent.Kind)
	}
	if llm.ParentID != agent.ID {
		t.Fatalf("expected llm parent to be agent")
	}
	if tool.ParentID != llm.ID {
		t.Fatalf("expected tool parent to be llm")
	}
	if tool.Attributes["tool.output"] != "results" {
		t.Fatalf("expected tool output attr")
	}
}

func TestTracer_RecordsError(t *testing.T) {
	tr := NewTracer()

	_, finish := tr.Start(context.Background(), "run", SpanKindRun)
	finish(errors.New("boom"))

	spans := tr.Spans()
	if len(spans) != 1 {
		t.Fatal("expected one span")
	}
	if spans[0].Err == nil {
		t.Fatal("expected error on span")
	}
}

func TestCurrentSpan_NoTracer(t *testing.T) {
	ctx := context.Background()
	if s := CurrentSpan(ctx); s != nil {
		t.Fatalf("expected nil span, got %v", s)
	}
	SetAttr(ctx, "k", "v")  // should not panic
	AddEvent(ctx, "e", nil) // should not panic
}

func TestStartNode(t *testing.T) {
	tr := NewTracer()
	ctx := WithTracer(context.Background(), tr)

	ctx, finish := tr.StartNode(ctx, "classify", 3)
	finish(nil)

	spans := tr.Spans()
	if len(spans) != 1 || spans[0].Kind != SpanKindNode {
		t.Fatalf("expected node span, got %v", spans)
	}
	if spans[0].Attributes["node.step"] != 3 {
		t.Fatalf("expected step 3, got %v", spans[0].Attributes["node.step"])
	}
}
