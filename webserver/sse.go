// Package webserver provides HTTP utilities for streaming DMR workflow events
// (including A2UI widgets) over Server-Sent Events (SSE).
package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/workflow"
)

// SSEHeaders writes the standard SSE response headers.
func SSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
}

// WriteSSEEvent writes a single event to the response writer.
func WriteSSEEvent(w http.ResponseWriter, eventName string, data []byte) error {
	if eventName != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// StreamWorkflowEvents consumes a workflow EventStream and writes SSE events.
// The caller must have already written SSE headers.
func StreamWorkflowEvents(ctx context.Context, w http.ResponseWriter, stream workflow.EventStream, input any) error {
	for ev, err := range stream.RunEvents(ctx, workflow.NewContext(), input) {
		if err != nil {
			data, _ := json.Marshal(map[string]any{"error": err.Error()})
			_ = WriteSSEEvent(w, "stream_error", data)
			return err
		}
		if ev == nil {
			continue
		}
		data, jerr := json.Marshal(ev)
		if jerr != nil {
			continue
		}
		var eventName string
		switch ev.Type {
		case workflow.EventTypeUIWidget:
			eventName = "ui_widget"
		case workflow.EventTypeToolCall:
			eventName = "tool_call"
		case workflow.EventTypeWorkflowStart:
			eventName = "workflow_start"
		case workflow.EventTypeWorkflowEnd:
			eventName = "workflow_end"
		case workflow.EventTypeNodeStart:
			eventName = "node_start"
		case workflow.EventTypeNodeEnd:
			eventName = "node_end"
		case workflow.EventTypeNodeSkip:
			eventName = "node_skip"
		case workflow.EventTypeStateDelta:
			eventName = "state_delta"
		case workflow.EventTypeInterrupt:
			eventName = "interrupt"
		}
		if err := WriteSSEEvent(w, eventName, data); err != nil {
			return err
		}
		if ev.Type == workflow.EventTypeWorkflowEnd {
			break
		}
	}
	return nil
}

// UIWidgetOnlyStream filters an EventStream to yield only UI widget events
// with a small debounce window so rapid successive updates are batched.
func UIWidgetOnlyStream(stream workflow.EventStream, debounce time.Duration) workflow.EventStream {
	return &uiWidgetStream{stream: stream, debounce: debounce}
}

type uiWidgetStream struct {
	stream   workflow.EventStream
	debounce time.Duration
}

func (u *uiWidgetStream) Run(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
	return u.stream.Run(ctx, wctx, input)
}

func (u *uiWidgetStream) RunEvents(ctx context.Context, wctx *workflow.Context, input any) iter.Seq2[*workflow.Event, error] {
	return func(yield func(*workflow.Event, error) bool) {
		var batch []*workflow.Event

		flush := func() bool {
			for _, ev := range batch {
				if !yield(ev, nil) {
					return false
				}
			}
			batch = batch[:0]
			return true
		}

		for ev, err := range u.stream.RunEvents(ctx, wctx, input) {
			if err != nil {
				if !flush() {
					return
				}
				if !yield(nil, err) {
					return
				}
				continue
			}
			if ev == nil {
				continue
			}
			if ev.Type != workflow.EventTypeUIWidget {
				continue
			}
			batch = append(batch, ev)
			if u.debounce <= 0 {
				if !flush() {
					return
				}
			}
		}
		flush()
	}
}

// UIToolTraceAndWidgetsStream passes through tool_call and workflow_end events immediately,
// batches ui_widget events (same debounce semantics as [UIWidgetOnlyStream]), and drops
// other workflow noise (workflow_start, node_start, etc.). Flushes pending widget batches
// before each tool_call, node_end (with assistant text output), or workflow_end so ordering
// matches execution.
func UIToolTraceAndWidgetsStream(stream workflow.EventStream, debounce time.Duration) workflow.EventStream {
	return &toolTraceWidgetStream{stream: stream, debounce: debounce}
}

func nodeEndShouldForward(ev *workflow.Event) bool {
	if ev == nil || ev.Type != workflow.EventTypeNodeEnd {
		return false
	}
	if ev.Output == nil {
		return false
	}
	s, ok := ev.Output.(string)
	if ok {
		return strings.TrimSpace(s) != ""
	}
	return strings.TrimSpace(fmt.Sprint(ev.Output)) != ""
}

type toolTraceWidgetStream struct {
	stream   workflow.EventStream
	debounce time.Duration
}

func (u *toolTraceWidgetStream) Run(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
	return u.stream.Run(ctx, wctx, input)
}

func (u *toolTraceWidgetStream) RunEvents(ctx context.Context, wctx *workflow.Context, input any) iter.Seq2[*workflow.Event, error] {
	return func(yield func(*workflow.Event, error) bool) {
		var batch []*workflow.Event

		flushWidgets := func() bool {
			for _, ev := range batch {
				if !yield(ev, nil) {
					return false
				}
			}
			batch = batch[:0]
			return true
		}

		for ev, err := range u.stream.RunEvents(ctx, wctx, input) {
			if err != nil {
				if !flushWidgets() {
					return
				}
				if !yield(nil, err) {
					return
				}
				continue
			}
			if ev == nil {
				continue
			}
			switch ev.Type {
			case workflow.EventTypeToolCall, workflow.EventTypeWorkflowEnd:
				if !flushWidgets() {
					return
				}
				if !yield(ev, nil) {
					return
				}
			case workflow.EventTypeNodeEnd:
				if !nodeEndShouldForward(ev) {
					break
				}
				if !flushWidgets() {
					return
				}
				if !yield(ev, nil) {
					return
				}
			case workflow.EventTypeUIWidget:
				batch = append(batch, ev)
				if u.debounce <= 0 {
					if !flushWidgets() {
						return
					}
				}
			default:
				// omit workflow_start, node_start, empty node_end, etc.
			}
		}
		flushWidgets()
	}
}
