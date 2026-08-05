package agent

import (
	"context"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// cancelFake blocks until the request context is cancelled.
type cancelFake struct{}

func (f *cancelFake) ChatCompletion(ctx context.Context, _ provider.ChatRequest) (*provider.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *cancelFake) ChatCompletionStream(context.Context, provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return nil, context.Canceled
}

func TestRunInterruptedOnContextCancel(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)

	fake := &cancelFake{}
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "test-model", MaxRetries: 0})
	llmCore.SetClientForModel("test-model", fake)
	chat := client.NewChatClient(llmCore, tool.NewToolExecutor(), tm)

	a := New(chat, tm, NopHooks(), Config{
		MaxSteps: 5,
		Models: []config.ModelConfig{{
			Name:    "test-model",
			Model:   "test-model",
			Default: true,
		}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	const tapeName = "interrupt-test"
	_, err := a.Run(ctx, tapeName, "hello", 0)
	if err == nil {
		t.Fatal("expected error from cancelled run")
	}

	entries, err := store.FetchAll(tapeName, nil)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	var execID string
	var foundInterrupt bool
	for _, e := range entries {
		switch e.Kind {
		case "exec_start":
			if id, _ := e.Payload["exec_id"].(string); id != "" {
				execID = id
			}
		case "event":
			if name, _ := e.Payload["name"].(string); name != tape.EventRunInterrupted {
				continue
			}
			foundInterrupt = true
			data, _ := e.Payload["data"].(map[string]any)
			if data == nil {
				t.Fatal("run_interrupted event missing data")
			}
			if got, _ := data["exec_id"].(string); got != execID {
				t.Errorf("run_interrupted exec_id = %q, want %q", got, execID)
			}
			if reason, _ := data["reason"].(string); reason != "cancelled" {
				t.Errorf("run_interrupted reason = %q, want cancelled", reason)
			}
		}
	}
	if execID == "" {
		t.Fatal("expected exec_start entry with exec_id")
	}
	if !foundInterrupt {
		t.Fatal("expected run_interrupted event on tape")
	}
}

func TestFilterAllowedTools(t *testing.T) {
	tools := []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "shell"}},
		{Spec: tool.ToolSpec{Name: "fsRead"}},
		{Spec: tool.ToolSpec{Name: "memoryRead"}},
	}

	// Empty allowed list → no filtering
	out := filterAllowedTools(tools, nil)
	if len(out) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(out))
	}

	// Allowed list with specific tools
	out = filterAllowedTools(tools, &runMode{toolWhitelist: true, allowedToolNames: map[string]struct{}{"memoryRead": {}, "shell": {}}})
	if len(out) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(out))
	}
	for _, o := range out {
		if o.Spec.Name != "memoryRead" && o.Spec.Name != "shell" {
			t.Errorf("unexpected tool %q allowed through", o.Spec.Name)
		}
	}

	// Allowed list with no matches
	out = filterAllowedTools(tools, &runMode{toolWhitelist: true, allowedToolNames: map[string]struct{}{"unknown": {}}})
	if len(out) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(out))
	}

	// Explicit empty whitelist removes all tools
	out = filterAllowedTools(tools, &runMode{toolWhitelist: true, allowedToolNames: map[string]struct{}{}})
	if len(out) != 0 {
		t.Fatalf("expected 0 tools for empty whitelist, got %d", len(out))
	}

	// allowedToolNames populated but whitelist off behaves like unrestricted (historical callers)
	out = filterAllowedTools(tools, &runMode{allowedToolNames: map[string]struct{}{"unknown": {}}})
	if len(out) != 3 {
		t.Fatalf("expected 3 tools when toolWhitelist unset, got %d", len(out))
	}
}

func TestHasShellFailure(t *testing.T) {
	failureResult := "❌ COMMAND FAILED (exit code: 1)\noutput"

	// Shell tool failure → true
	calls := []core.ToolCallData{{Function: core.ToolCallFunction{Name: "shell"}}}
	results := []any{failureResult}
	if !hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=true for shell tool failure")
	}

	// Powershell tool failure → true
	calls = []core.ToolCallData{{Function: core.ToolCallFunction{Name: "powershellOutput"}}}
	results = []any{failureResult}
	if !hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=true for powershell tool failure")
	}

	// fsRead tool containing same string → false (must not false-positive)
	calls = []core.ToolCallData{{Function: core.ToolCallFunction{Name: "fsRead"}}}
	results = []any{failureResult}
	if hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=false for fsRead tool")
	}

	// No calls available but content matches → conservatively true (unknown tool)
	calls = nil
	results = []any{failureResult}
	if !hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=true when content matches even without calls")
	}

	// Non-failure content → false
	calls = []core.ToolCallData{{Function: core.ToolCallFunction{Name: "shell"}}}
	results = []any{"some normal output"}
	if hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=false for normal output")
	}
}
