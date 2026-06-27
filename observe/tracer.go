// Package observe provides a lightweight, OpenTelemetry-aligned span model for
// DMR. It does not depend on the OpenTelemetry SDK; instead it exposes a thin
// facade that can be backed by an actual OTel tracer or by the built-in
// in-memory recorder.
//
// The intended hierarchy is:
//
//	run (root)
//	└── agent:{id}
//	    ├── llm_call:{provider}/{model}
//	    │   └── tool_call:{name}
//	    └── node:{name}   (workflow layer)
package observe

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SpanKind classifies spans in the DMR trace tree.
type SpanKind string

const (
	SpanKindRun      SpanKind = "run"
	SpanKindAgent    SpanKind = "agent"
	SpanKindLLMCall  SpanKind = "llm_call"
	SpanKindToolCall SpanKind = "tool_call"
	SpanKindNode     SpanKind = "node"
	SpanKindWorkflow SpanKind = "workflow"
)

// Span represents one unit of work in a trace.
type Span struct {
	ID         string
	Name       string
	Kind       SpanKind
	ParentID   string
	Start      time.Time
	End        time.Time
	Attributes map[string]any
	Events     []SpanEvent
	Err        error
}

// SpanEvent is a timestamped annotation on a span.
type SpanEvent struct {
	Name       string
	Timestamp  time.Time
	Attributes map[string]any
}

// Tracer is the entry point for creating spans.
type Tracer struct {
	mu     sync.RWMutex
	spans  []*Span
	nextID int
}

// NewTracer creates an in-memory tracer.
func NewTracer() *Tracer {
	return &Tracer{}
}

// Start creates a span. The returned finish function records the end time and
// any error. Finish is safe to call with a nil error.
func (t *Tracer) Start(ctx context.Context, name string, kind SpanKind) (context.Context, func(error)) {
	parent := SpanFromContext(ctx)
	span := &Span{
		ID:         t.nextSpanID(),
		Name:       name,
		Kind:       kind,
		ParentID:   parent,
		Start:      time.Now(),
		Attributes: make(map[string]any),
	}
	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()

	ctx = context.WithValue(ctx, spanContextKey, span.ID)
	finish := func(err error) {
		span.End = time.Now()
		span.Err = err
	}
	return ctx, finish
}

// StartAgent starts an agent span.
func (t *Tracer) StartAgent(ctx context.Context, id, role string) (context.Context, func(error)) {
	ctx, finish := t.Start(ctx, id, SpanKindAgent)
	CurrentSpan(ctx).SetAttr("agent.role", role)
	return ctx, finish
}

// StartLLMCall starts a span for a single LLM request.
func (t *Tracer) StartLLMCall(ctx context.Context, provider, model string) (context.Context, func(totalTokens int, err error)) {
	ctx, finish := t.Start(ctx, fmt.Sprintf("%s/%s", provider, model), SpanKindLLMCall)
	span := CurrentSpan(ctx)
	span.SetAttr("llm.provider", provider)
	span.SetAttr("llm.model", model)
	return ctx, func(totalTokens int, err error) {
		span.SetAttr("llm.tokens", totalTokens)
		finish(err)
	}
}

// StartToolCall starts a span for a single tool execution.
func (t *Tracer) StartToolCall(ctx context.Context, name, input string) (context.Context, func(output string, err error)) {
	ctx, finish := t.Start(ctx, name, SpanKindToolCall)
	span := CurrentSpan(ctx)
	span.SetAttr("tool.name", name)
	span.SetAttr("tool.input", input)
	return ctx, func(output string, err error) {
		span.SetAttr("tool.output", output)
		finish(err)
	}
}

// StartNode starts a span for a workflow node.
func (t *Tracer) StartNode(ctx context.Context, name string, step int) (context.Context, func(error)) {
	ctx, finish := t.Start(ctx, name, SpanKindNode)
	CurrentSpan(ctx).SetAttr("node.step", step)
	return ctx, finish
}

// SetAttr sets an attribute on the current span (no-op if no span is in ctx).
func SetAttr(ctx context.Context, key string, value any) {
	if s := CurrentSpan(ctx); s != nil {
		s.SetAttr(key, value)
	}
}

// AddEvent adds an event to the current span (no-op if no span is in ctx).
func AddEvent(ctx context.Context, name string, attrs map[string]any) {
	if s := CurrentSpan(ctx); s != nil {
		s.AddEvent(name, attrs)
	}
}

// CurrentSpan returns the span active in ctx, or nil.
func CurrentSpan(ctx context.Context) *Span {
	id := SpanFromContext(ctx)
	if id == "" {
		return nil
	}
	return tFrom(ctx).FindSpan(id)
}

// SpanFromContext returns the span ID stored in ctx, or empty.
func SpanFromContext(ctx context.Context) string {
	if v := ctx.Value(spanContextKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Spans returns a snapshot of recorded spans.
func (t *Tracer) Spans() []*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*Span, len(t.spans))
	copy(out, t.spans)
	return out
}

// FindSpan looks up a span by ID.
func (t *Tracer) FindSpan(id string) *Span {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, s := range t.spans {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// SetAttr sets an attribute on the span.
func (s *Span) SetAttr(key string, value any) {
	if s == nil {
		return
	}
	if s.Attributes == nil {
		s.Attributes = make(map[string]any)
	}
	s.Attributes[key] = value
}

// AddEvent records a timestamped event on the span.
func (s *Span) AddEvent(name string, attrs map[string]any) {
	if s == nil {
		return
	}
	s.Events = append(s.Events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

func (t *Tracer) nextSpanID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	return fmt.Sprintf("span-%d", t.nextID)
}

type contextKey string

const spanContextKey contextKey = "observe:span"

// tFrom attempts to locate a Tracer from the context. If none is present, a
// new no-op tracer is returned. Callers that need the actual recorder should
// pass the tracer explicitly.
func tFrom(ctx context.Context) *Tracer {
	if v := ctx.Value(tracerContextKey); v != nil {
		if t, ok := v.(*Tracer); ok {
			return t
		}
	}
	return NewTracer()
}

// WithTracer stores a tracer in the context.
func WithTracer(ctx context.Context, t *Tracer) context.Context {
	return context.WithValue(ctx, tracerContextKey, t)
}

// TracerFromContext returns the tracer stored in ctx, or nil.
func TracerFromContext(ctx context.Context) *Tracer {
	if v := ctx.Value(tracerContextKey); v != nil {
		if t, ok := v.(*Tracer); ok {
			return t
		}
	}
	return nil
}

type tracerKey string

const tracerContextKey tracerKey = "observe:tracer"
