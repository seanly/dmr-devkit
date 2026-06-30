package agent

import (
	"context"
	"errors"
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

func TestCountSubagentDepth(t *testing.T) {
	cases := []struct {
		tape  string
		depth int
	}{
		{"main", 0},
		{"main:subagent:abc123", 1},
		{"main:subagent:abc123:subagent:def456", 2},
		{"main:subagent:abc:subagent:def:subagent:ghi", 3},
	}
	for _, c := range cases {
		got := countSubagentDepth(c.tape)
		if got != c.depth {
			t.Errorf("countSubagentDepth(%q) = %d, want %d", c.tape, got, c.depth)
		}
	}
}

func TestRunSubagentRejectsAtMaxDepth(t *testing.T) {
	a := New(nil, nil, nil, Config{})
	// Depth 3 should be rejected before any run logic.
	_, err := a.RunSubagent(nil, "main:subagent:a:subagent:b:subagent:c", "task", "", "temp", "", 0)
	if err == nil || !core.IsErrorKind(err, core.ErrDenied) || !strings.Contains(err.Error(), "max nesting depth 3 reached") {
		t.Fatalf("want depth error at depth 3, got %v", err)
	}
}

// fakeSubagentLLMClient is a minimal provider client that returns queued responses.
type fakeSubagentLLMClient struct {
	queue []any
	pos   int
}

func (f *fakeSubagentLLMClient) ChatCompletion(_ context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	if f.pos >= len(f.queue) {
		return nil, errors.New("no queued completion")
	}
	item := f.queue[f.pos]
	f.pos++
	switch v := item.(type) {
	case *provider.ChatResponse:
		return v, nil
	case error:
		return nil, v
	default:
		return nil, fmt.Errorf("unexpected type: %T", item)
	}
}

func (f *fakeSubagentLLMClient) ChatCompletionStream(_ context.Context, _ provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func newSubagentTestAgent(t *testing.T, fake *fakeSubagentLLMClient, tools []*tool.Tool) *Agent {
	t.Helper()
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "test-model", MaxRetries: 0})
	llmCore.SetClientForModel("test-model", fake)
	chat := client.NewChatClient(llmCore, tool.NewToolExecutor(), nil)
	return New(chat, tape.NewTapeManager(tape.NewInMemoryTapeStore()), nil, Config{
		Workspace: t.TempDir(),
		AgentPolicy: config.AgentConfig{
			ToolResultMaxChars: 10,
			ToolResultPolicy: config.ToolResultPolicyConfig{
				PerMessageBudget: 50,
				PreviewChars:     5,
			},
		},
		Models: []config.ModelConfig{
			{Name: "test-model", Model: "test-model", Default: true},
		},
		Tools: tools,
	})
}

func findChildTape(store tape.TapeStore, parent string) string {
	for _, name := range store.ListTapes() {
		if strings.HasPrefix(name, parent+":"+SubagentTapeSuffix+":") {
			return name
		}
	}
	return ""
}

func TestRunSubagent_IsolatesToolResultManagerState(t *testing.T) {
	bigOutput := strings.Repeat("x", 100)
	bigTool := &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "bigTool",
			Description: "returns a large payload",
			Parameters: map[string]any{
				"type":     "object",
				"properties": map[string]any{},
			},
		},
		Handler: func(_ *tool.ToolContext, _ map[string]any) (any, error) {
			return bigOutput, nil
		},
	}

	fake := &fakeSubagentLLMClient{queue: []any{
		&provider.ChatResponse{
			ToolCalls: []provider.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: provider.ToolCallFunction{
					Name:      "bigTool",
					Arguments: "{}",
				},
			}},
		},
		&provider.ChatResponse{Text: "done"},
		&provider.ChatResponse{Text: "done"},
		&provider.ChatResponse{Text: "done"},
	}}

	a := newSubagentTestAgent(t, fake, []*tool.Tool{bigTool})
	parentTape := "parent"

	_, err := a.RunSubagentWithTools(context.Background(), parentTape, "run big tool", "", "temp", "", 0, []string{"bigTool"}, nil)
	if err != nil {
		t.Fatalf("subagent run failed: %v", err)
	}

	childTape := findChildTape(a.tape.Store, parentTape)
	if childTape == "" {
		t.Fatal("child tape not found")
	}

	// Verify the child tape actually externalized the result.
	childEntries, err := a.tape.Store.FetchAll(childTape, nil)
	if err != nil {
		t.Fatalf("fetch child entries: %v", err)
	}
	var foundReplacement bool
	for _, e := range childEntries {
		if e.Kind == "content_replacement" {
			if repl, ok := e.Payload["replacement"].(string); ok && strings.HasPrefix(repl, "<persisted-output>") {
				foundReplacement = true
				break
			}
		}
	}
	if !foundReplacement {
		t.Fatalf("expected externalized content_replacement on child tape, got entries: %v", childEntries)
	}

	// Now exercise the parent manager with the same raw content on the child tape.
	// If the parent manager had been mutated by the child, it would already have a
	// replacement for call-1 and ApplyTurnBudget would return nothing. With proper
	// isolation it must perform a fresh persistence and return a replacement.
	msgs := []map[string]any{
		{
			"role":       "assistant",
			"content":    "",
			"tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "bigTool", "arguments": "{}"}}},
		},
		{
			"role":         "tool",
			"tool_call_id": "call-1",
			"content":      bigOutput,
		},
	}
	repls := a.toolResults.ApplyTurnBudget(childTape, msgs)
	if len(repls) == 0 {
		t.Fatal("parent manager already had child replacement; tool-result state leaked from subagent")
	}
}

func TestRunSubagent_ErrorAppendsFailureHandoffPacket(t *testing.T) {
	wantErr := errors.New("llm exploded")
	fake := &fakeSubagentLLMClient{queue: []any{wantErr}}

	a := newSubagentTestAgent(t, fake, nil)
	parentTape := "parent"

	res, err := a.RunSubagentWithTools(context.Background(), parentTape, "fail please", "", "temp", "", 0, nil, nil)
	if err == nil {
		t.Fatal("expected subagent error")
	}
	if res == nil || res.Packet == nil {
		t.Fatal("expected failure packet in result")
	}
	if !strings.Contains(res.Packet.Summary, "subagent failed") {
		t.Fatalf("unexpected packet summary: %q", res.Packet.Summary)
	}

	childTape := findChildTape(a.tape.Store, parentTape)
	if childTape == "" {
		t.Fatal("child tape not found")
	}
	entries, err := a.tape.Store.FetchAll(childTape, nil)
	if err != nil {
		t.Fatalf("fetch child entries: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Kind == "handoff_packet" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected handoff_packet entry on child tape for failed subagent")
	}
}
