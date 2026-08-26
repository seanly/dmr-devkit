package devkit

import (
	"context"
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/webserver"
	"github.com/seanly/dmr-devkit/workflow"
)

// queuedFake replays ChatCompletion responses in order.
type queuedFake struct {
	responses []*provider.ChatResponse
	pos       int
}

func (f *queuedFake) ChatCompletion(_ context.Context, _ provider.ChatRequest) (*provider.ChatResponse, error) {
	if f.pos >= len(f.responses) {
		return &provider.ChatResponse{Text: "Done."}, nil
	}
	r := f.responses[f.pos]
	f.pos++
	return r, nil
}

func (f *queuedFake) ChatCompletionStream(context.Context, provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return nil, fmt.Errorf("stream not implemented")
}

func echoTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "echo",
			Description: "echo input",
			Group:       tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
				"required": []string{"message"},
			},
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			msg, _ := args["message"].(string)
			return map[string]any{"content": msg}, nil
		},
	}
}

func newEchoKit(t *testing.T, fake *queuedFake) *Kit {
	t.Helper()
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "test-model", MaxRetries: 0})
	llmCore.SetClientForModel("test-model", fake)
	exec := tool.NewToolExecutor()
	chat := client.NewChatClient(llmCore, exec, tm)
	a := agent.New(chat, tm, agent.NopHooks(), agent.Config{
		MaxSteps: 5,
		Models: []config.ModelConfig{{
			Name:    "test-model",
			Model:   "test-model",
			Default: true,
		}},
		Tools: []*tool.Tool{echoTool()},
	})
	a.SetExecutor(exec)
	return &Kit{Agent: a, TapeManager: tm, Store: store, Client: chat}
}

func twoEchoCalls() *provider.ChatResponse {
	return &provider.ChatResponse{
		ToolCalls: []provider.ToolCall{
			{
				ID:   "c1",
				Type: "function",
				Function: provider.ToolCallFunction{
					Name:      "echo",
					Arguments: `{"message":"a"}`,
				},
			},
			{
				ID:   "c2",
				Type: "function",
				Function: provider.ToolCallFunction{
					Name:      "echo",
					Arguments: `{"message":"b"}`,
				},
			},
		},
	}
}

// TestAgentNodeRunEventsHonorsYieldStop reproduces the HTTP SSE panic:
// range function continued iteration after function for loop body returned false.
// The consumer stops after the first tool_call (client disconnect / write failure);
// a second OnToolCall in the same turn must not yield again.
func TestAgentNodeRunEventsHonorsYieldStop(t *testing.T) {
	kit := newEchoKit(t, &queuedFake{responses: []*provider.ChatResponse{twoEchoCalls()}})
	node := kit.AsAgentNodeWithTape("chat", "tape1")
	filtered := webserver.UIToolTraceAndWidgetsStream(node, 0)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RunEvents panicked after consumer stopped: %v", r)
		}
	}()

	toolCalls := 0
	for ev, err := range filtered.RunEvents(context.Background(), workflow.NewContext(), "hi") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev != nil && ev.Type == workflow.EventTypeToolCall {
			toolCalls++
			break
		}
	}
	if toolCalls != 1 {
		t.Fatalf("got %d tool_call events before break, want 1", toolCalls)
	}
}

func TestAgentNodeRunEventsDirectHonorsYieldStop(t *testing.T) {
	kit := newEchoKit(t, &queuedFake{responses: []*provider.ChatResponse{twoEchoCalls()}})
	node := kit.AsAgentNodeWithTape("chat", "tape1")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RunEvents panicked after consumer stopped: %v", r)
		}
	}()

	toolCalls := 0
	for ev, err := range node.RunEvents(context.Background(), workflow.NewContext(), "hi") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev != nil && ev.Type == workflow.EventTypeToolCall {
			toolCalls++
			break
		}
	}
	if toolCalls != 1 {
		t.Fatalf("got %d tool_call events before break, want 1", toolCalls)
	}
}
