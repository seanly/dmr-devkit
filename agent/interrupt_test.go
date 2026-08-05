package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

func TestRunInterruptedOnPreemptCause(t *testing.T) {
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

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(ErrRunPreempted)

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel(ErrRunPreempted)
	}()

	const tapeName = "interrupt-preempt-test"
	_, err := a.Run(ctx, tapeName, "hello", 0)
	if err == nil {
		t.Fatal("expected error from cancelled run")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(context.Cause(ctx), ErrRunPreempted) {
		t.Fatalf("expected cancel/preempt, got %v cause=%v", err, context.Cause(ctx))
	}

	entries, err := store.FetchAll(tapeName, nil)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	var foundInterrupt bool
	for _, e := range entries {
		if e.Kind != "event" {
			continue
		}
		if name, _ := e.Payload["name"].(string); name != tape.EventRunInterrupted {
			continue
		}
		foundInterrupt = true
		data, _ := e.Payload["data"].(map[string]any)
		if data == nil {
			t.Fatal("run_interrupted event missing data")
		}
		if reason, _ := data["reason"].(string); reason != InterruptReasonPreempted {
			t.Errorf("reason = %q, want %q", reason, InterruptReasonPreempted)
		}
		if suppress, _ := data["suppress_notice"].(bool); !suppress {
			t.Errorf("expected suppress_notice=true, got %#v", data)
		}
	}
	if !foundInterrupt {
		t.Fatal("expected run_interrupted event on tape")
	}
}
