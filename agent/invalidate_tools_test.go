package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

type countingHooks struct {
	noopHooks
	collectCalls int
}

func (h *countingHooks) CollectAllTools(_ context.Context, includeCore, includeExtended bool) []*tool.Tool {
	if includeExtended {
		h.collectCalls++
		return []*tool.Tool{{
			Spec: tool.ToolSpec{Name: "ext_tool"},
		}}
	}
	return nil
}

func TestInvalidateExtendedTools(t *testing.T) {
	h := &countingHooks{}
	a := &Agent{hooks: h}

	first := a.GetAllExtendedTools()
	if len(first) != 1 || h.collectCalls != 1 {
		t.Fatalf("first load: tools=%d collectCalls=%d", len(first), h.collectCalls)
	}

	// Cached — no second collect.
	second := a.GetAllExtendedTools()
	if len(second) != 1 || h.collectCalls != 1 {
		t.Fatalf("cached load: collectCalls=%d", h.collectCalls)
	}

	a.InvalidateExtendedTools()

	third := a.GetAllExtendedTools()
	if len(third) != 1 || h.collectCalls != 2 {
		t.Fatalf("after invalidate: collectCalls=%d", h.collectCalls)
	}
}

func TestInvalidateExtendedToolsConcurrent(t *testing.T) {
	h := &countingHooks{}
	a := &Agent{hooks: h}
	_ = a.GetAllExtendedTools()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.InvalidateExtendedTools()
			_ = a.GetAllExtendedTools()
		}()
	}
	wg.Wait()
	if h.collectCalls < 2 {
		t.Fatalf("expected at least 2 collect calls, got %d", h.collectCalls)
	}
}
