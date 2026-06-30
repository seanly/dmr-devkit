package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// multiTurnResponse describes one queued LLM response.
type multiTurnResponse struct {
	text      string
	toolCalls []provider.ToolCall
	usage     provider.Usage
}

// multiTurnFake is a provider.ChatProvider that replays a queued sequence of
// responses. It records every request so tests can inspect the conversation.
// When the queue is exhausted and the latest message is a tool result, it
// returns a deterministic "Done." reply.
type multiTurnFake struct {
	calls     []provider.ChatRequest
	responses []multiTurnResponse
	pos       int
}

func (f *multiTurnFake) ChatCompletion(_ context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	f.calls = append(f.calls, req)

	// Summarizer requests are injected by proactive compaction. Return a
	// generic, valid summary without consuming the agent-loop queue.
	if isSummarizerRequest(req) {
		return &provider.ChatResponse{
			Text:  "<summary>Continuing the task: " + extractGoalHint(req) + "</summary>",
			Usage: &provider.Usage{PromptTokens: 200, CompletionTokens: 20, TotalTokens: 220},
		}, nil
	}

	if f.pos < len(f.responses) {
		r := f.responses[f.pos]
		f.pos++
		return &provider.ChatResponse{
			Text:      r.text,
			ToolCalls: r.toolCalls,
			Usage:     &r.usage,
		}, nil
	}

	// Fallback when the queue is exhausted.
	if len(req.Messages) > 0 {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" {
			return &provider.ChatResponse{
				Text:  "Done.",
				Usage: &provider.Usage{PromptTokens: 2500, CompletionTokens: 2, TotalTokens: 2502},
			}, nil
		}
	}
	return &provider.ChatResponse{
		Text:  "Acknowledged.",
		Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
	}, nil
}

func (f *multiTurnFake) ChatCompletionStream(_ context.Context, _ provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return nil, fmt.Errorf("stream not implemented")
}

func isSummarizerRequest(req provider.ChatRequest) bool {
	for _, m := range req.Messages {
		content := m.Content
		if strings.Contains(content, "=== 总结任务 ===") || strings.Contains(content, "conversation summarizer") {
			return true
		}
	}
	return false
}

func extractGoalHint(req provider.ChatRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return firstNWords(m.Content, 6)
		}
	}
	return "task"
}

func firstNWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) <= n {
		return s
	}
	return strings.Join(fields[:n], " ")
}

func dummyEchoTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "echo",
			Description: "echo input",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
				"required": []string{"message"},
			},
		},
		Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
			msg, _ := args["message"].(string)
			return map[string]any{"content": msg}, nil
		},
	}
}

func newIntegrationAgent(t *testing.T, fake *multiTurnFake, tm *tape.TapeManager) *Agent {
	t.Helper()
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "test-model", MaxRetries: 0})
	llmCore.SetClientForModel("test-model", fake)
	exec := tool.NewToolExecutor()
	chat := client.NewChatClient(llmCore, exec, tm)

	enabled := true
	a := New(chat, tm, NopHooks(), Config{
		MaxSteps: 20,
		AgentPolicy: config.AgentConfig{
			MaxToken:         4000,
			HandoffThreshold: 0.5,
			Handoff: config.HandoffConfig{
				StateEnabled:      &enabled,
				CompactAfterState: true,
				StateUpdate:       "heuristic",
			},
			Scaffolding: config.ScaffoldingConfig{Profile: "standard"},
		},
		Models: []config.ModelConfig{
			{
				Name:             "test-model",
				Model:            "test-model",
				Default:          true,
				MaxToken:         4000,
				HandoffThreshold: 0.5,
			},
		},
		Tools: []*tool.Tool{dummyEchoTool()},
	})
	a.SetExecutor(exec)
	return a
}

func TestLongSessionWithCompact(t *testing.T) {
	ctx := context.Background()
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)

	var responses []multiTurnResponse
	for i := 1; i <= 7; i++ {
		responses = append(responses, multiTurnResponse{
			toolCalls: []provider.ToolCall{
				{
					ID:   fmt.Sprintf("call_%d", i),
					Type: "function",
					Function: provider.ToolCallFunction{
						Name:      "echo",
						Arguments: fmt.Sprintf(`{"message":"round %d"}`, i),
					},
				},
			},
			usage: provider.Usage{PromptTokens: 500 + (i-1)*300, CompletionTokens: 5, TotalTokens: 505 + (i-1)*300},
		})
	}
	responses = append(responses, multiTurnResponse{
		text:  "Task complete.",
		usage: provider.Usage{PromptTokens: 100, CompletionTokens: 3, TotalTokens: 103},
	})

	fake := &multiTurnFake{responses: responses}
	a := newIntegrationAgent(t, fake, tm)

	const tapeName = "long-session-test"
	result, err := a.Run(ctx, tapeName, "Please run the echo tool repeatedly until done.", 0)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(strings.ToLower(result.Output), "complete") {
		t.Fatalf("expected result output to contain 'complete', got %q", result.Output)
	}

	entries, err := store.FetchAll(tapeName, nil)
	if err != nil {
		t.Fatalf("FetchAll failed: %v", err)
	}

	var anchors, compactSummaries int
	for _, e := range entries {
		switch e.Kind {
		case "anchor":
			anchors++
		case "compact_summary":
			compactSummaries++
		}
	}
	if anchors == 0 {
		t.Errorf("expected at least one anchor entry")
	}
	if compactSummaries == 0 {
		t.Errorf("expected at least one compact_summary entry")
	}

	st := a.latestTaskState(tapeName)
	if st == nil {
		t.Fatal("expected latest task_state")
	}
	goalLower := strings.ToLower(st.Goal)
	if !strings.Contains(goalLower, "echo") && !strings.Contains(goalLower, "repeatedly") {
		t.Fatalf("expected goal to contain 'echo' or 'repeatedly', got %q", st.Goal)
	}
}

func TestLongSessionResumePreservesDiscoveredTools(t *testing.T) {
	ctx := context.Background()
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)

	fake := &multiTurnFake{}
	a := newIntegrationAgent(t, fake, tm)

	const tapeName = "resume-tools-test"
	a.DiscoverTool(tapeName, "echo")
	a.persistTapeState(tapeName)

	// Simulate a fresh process using the same tape store.
	fresh := newIntegrationAgent(t, fake, tm)
	fresh.restoreTapeState(tapeName)

	if !fresh.IsToolDiscovered(tapeName, "echo") {
		t.Errorf("expected echo to be discovered after restore")
	}

	entries, err := store.FetchAll(tapeName, nil)
	if err != nil {
		t.Fatalf("FetchAll failed: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Kind != "agent_state" {
			continue
		}
		raw, err := json.Marshal(e.Payload["discovered_tools"])
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), "echo") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected agent_state entry with echo in discovered_tools")
	}

	_ = ctx
}

func TestClientTruncationLastResort(t *testing.T) {
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "test-model", MaxRetries: 0})
	fake := &multiTurnFake{}
	llmCore.SetClientForModel("test-model", fake)
	chat := client.NewChatClient(llmCore, tool.NewToolExecutor(), nil)

	var msgs []map[string]any
	msgs = append(msgs, map[string]any{"role": "system", "content": "You are a helpful assistant."})
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		// Each long message contributes enough tokens to exceed a tiny limit.
		content := strings.Repeat(fmt.Sprintf("paragraph %d contains a lot of words. ", i), 50)
		msgs = append(msgs, map[string]any{"role": role, "content": content})
	}

	_, err := chat.ChatRaw(context.Background(), client.ChatOpts{
		Messages:     msgs,
		ContextLimit: 80,
	})
	if err != nil {
		t.Fatalf("ChatRaw failed: %v", err)
	}

	if len(fake.calls) == 0 {
		t.Fatal("expected fake provider to record a call")
	}
	got := len(fake.calls[0].Messages)
	if got >= len(msgs) {
		t.Fatalf("expected truncated message count, got %d (original %d)", got, len(msgs))
	}
}

func TestCompactPreservesGoal(t *testing.T) {
	ctx := context.Background()
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)

	goal := "book a flight to Paris"
	responses := []multiTurnResponse{
		{
			toolCalls: []provider.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: provider.ToolCallFunction{
						Name:      "echo",
						Arguments: `{"message":"search flights to Paris"}`,
					},
				},
			},
			usage: provider.Usage{PromptTokens: 2200, CompletionTokens: 5, TotalTokens: 2205},
		},
		{
			text:  "I have booked your flight to Paris as requested.",
			usage: provider.Usage{PromptTokens: 120, CompletionTokens: 10, TotalTokens: 130},
		},
	}

	fake := &multiTurnFake{responses: responses}
	a := newIntegrationAgent(t, fake, tm)

	const tapeName = "compact-goal-test"
	result, err := a.Run(ctx, tapeName, goal, 0)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(strings.ToLower(result.Output), "paris") {
		t.Fatalf("expected final response to reference goal (Paris), got %q", result.Output)
	}

	st := a.latestTaskState(tapeName)
	if st == nil {
		t.Fatal("expected latest task_state")
	}
	if !strings.Contains(strings.ToLower(st.Goal), "paris") {
		t.Fatalf("expected preserved goal to contain 'paris', got %q", st.Goal)
	}
}
