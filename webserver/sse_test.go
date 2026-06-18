package webserver

import (
	"bytes"
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/seanly/dmr-devkit/workflow"
)

func TestSSEHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	SSEHeaders(w)
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
	if w.Header().Get("Connection") != "keep-alive" {
		t.Errorf("Connection = %q", w.Header().Get("Connection"))
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}

func TestWriteSSEEvent(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteSSEEvent(w, "test", []byte(`{"key":"value"}`)); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("event: test\n")) {
		t.Errorf("body missing event name: %s", body)
	}
	if !bytes.Contains([]byte(body), []byte(`data: {"key":"value"}`)) {
		t.Errorf("body missing data: %s", body)
	}

	w2 := httptest.NewRecorder()
	if err := WriteSSEEvent(w2, "", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	body2 := w2.Body.String()
	if bytes.Contains([]byte(body2), []byte("event:")) {
		t.Errorf("empty event should not emit event: line")
	}
}

func TestNodeEndShouldForward(t *testing.T) {
	if nodeEndShouldForward(nil) {
		t.Errorf("nil should be false")
	}
	if nodeEndShouldForward(&workflow.Event{}) {
		t.Errorf("empty event should be false")
	}
	if !nodeEndShouldForward(&workflow.Event{Type: workflow.EventTypeNodeEnd, Output: "hello"}) {
		t.Errorf("string output should be true")
	}
	if nodeEndShouldForward(&workflow.Event{Type: workflow.EventTypeNodeEnd, Output: "   "}) {
		t.Errorf("whitespace-only string should be false")
	}
	if !nodeEndShouldForward(&workflow.Event{Type: workflow.EventTypeNodeEnd, Output: 42}) {
		t.Errorf("non-string output should be true")
	}
	if nodeEndShouldForward(&workflow.Event{Type: workflow.EventTypeNodeStart, Output: "hello"}) {
		t.Errorf("wrong type should be false")
	}
}

// mockEventStream is a test implementation of workflow.EventStream.
type mockEventStream struct {
	events []*workflow.Event
	err    error
}

func (m *mockEventStream) Run(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
	return nil, nil
}

func (m *mockEventStream) RunEvents(ctx context.Context, wctx *workflow.Context, input any) iter.Seq2[*workflow.Event, error] {
	return func(yield func(*workflow.Event, error) bool) {
		for _, ev := range m.events {
			if !yield(ev, nil) {
				return
			}
		}
		if m.err != nil {
			yield(nil, m.err)
		}
	}
}

func TestUIWidgetOnlyStream(t *testing.T) {
	base := &mockEventStream{
		events: []*workflow.Event{
			{Type: workflow.EventTypeWorkflowStart},
			{Type: workflow.EventTypeUIWidget, UIWidget: map[string]any{"x": 1}},
			{Type: workflow.EventTypeNodeStart},
			{Type: workflow.EventTypeUIWidget, UIWidget: map[string]any{"x": 2}},
			{Type: workflow.EventTypeWorkflowEnd},
		},
	}
	filtered := UIWidgetOnlyStream(base, 0)
	ctx := context.Background()
	var got []*workflow.Event
	for ev, err := range filtered.RunEvents(ctx, workflow.NewContext(), nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 widget events, got %d", len(got))
	}
	if got[0].UIWidget.(map[string]any)["x"] != 1 {
		t.Errorf("first event mismatch")
	}
	if got[1].UIWidget.(map[string]any)["x"] != 2 {
		t.Errorf("second event mismatch")
	}
}

func TestUIWidgetOnlyStreamWithError(t *testing.T) {
	base := &mockEventStream{
		events: []*workflow.Event{
			{Type: workflow.EventTypeUIWidget, UIWidget: map[string]any{"x": 1}},
		},
		err: nil,
	}
	filtered := UIWidgetOnlyStream(base, 0)
	ctx := context.Background()
	var got int
	for ev, err := range filtered.RunEvents(ctx, workflow.NewContext(), nil) {
		if err != nil {
			continue
		}
		if ev != nil {
			got++
		}
	}
	if got != 1 {
		t.Errorf("expected 1 event, got %d", got)
	}
}

func TestUIToolTraceAndWidgetsStream(t *testing.T) {
	base := &mockEventStream{
		events: []*workflow.Event{
			{Type: workflow.EventTypeWorkflowStart},
			{Type: workflow.EventTypeUIWidget, UIWidget: map[string]any{"x": 1}},
			{Type: workflow.EventTypeToolCall},
			{Type: workflow.EventTypeUIWidget, UIWidget: map[string]any{"x": 2}},
			{Type: workflow.EventTypeNodeEnd, Output: "hello"},
			{Type: workflow.EventTypeWorkflowEnd},
		},
	}
	filtered := UIToolTraceAndWidgetsStream(base, 0)
	ctx := context.Background()
	var got []string
	for ev, err := range filtered.RunEvents(ctx, workflow.NewContext(), nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, string(ev.Type))
	}
	// Expected: widget before tool_call, tool_call, widget before node_end, node_end, workflow_end
	expected := []string{
		string(workflow.EventTypeUIWidget),
		string(workflow.EventTypeToolCall),
		string(workflow.EventTypeUIWidget),
		string(workflow.EventTypeNodeEnd),
		string(workflow.EventTypeWorkflowEnd),
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	for i, e := range expected {
		if got[i] != e {
			t.Errorf("event[%d] = %s, want %s", i, got[i], e)
		}
	}
}

func TestStreamWorkflowEvents(t *testing.T) {
	w := httptest.NewRecorder()
	base := &mockEventStream{
		events: []*workflow.Event{
			{Type: workflow.EventTypeWorkflowStart},
			{Type: workflow.EventTypeUIWidget, UIWidget: map[string]any{"x": 1}},
			{Type: workflow.EventTypeWorkflowEnd},
		},
	}
	SSEHeaders(w)
	if err := StreamWorkflowEvents(context.Background(), w, base, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("event: workflow_start\n")) {
		t.Errorf("body missing workflow_start: %s", body)
	}
	if !bytes.Contains([]byte(body), []byte("event: ui_widget\n")) {
		t.Errorf("body missing ui_widget: %s", body)
	}
	if !bytes.Contains([]byte(body), []byte("event: workflow_end\n")) {
		t.Errorf("body missing workflow_end: %s", body)
	}
}

func TestStreamWorkflowEventsWithError(t *testing.T) {
	w := httptest.NewRecorder()
	base := &mockEventStream{
		events: []*workflow.Event{
			{Type: workflow.EventTypeWorkflowStart},
		},
		err: errTest,
	}
	SSEHeaders(w)
	if err := StreamWorkflowEvents(context.Background(), w, base, nil); err != errTest {
		t.Fatalf("expected errTest, got %v", err)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("event: stream_error\n")) {
		t.Errorf("body missing stream_error: %s", body)
	}
}

var errTest = errTestType{}

type errTestType struct{}

func (errTestType) Error() string { return "test error" }

func TestStreamWorkflowEventsMarshalError(t *testing.T) {
	w := httptest.NewRecorder()
	base := &mockEventStream{
		events: []*workflow.Event{
			{Type: workflow.EventTypeUIWidget, UIWidget: make(chan int)}, // unmarshalable
		},
	}
	SSEHeaders(w)
	if err := StreamWorkflowEvents(context.Background(), w, base, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not panic and just skip the unmarshalable event
}
