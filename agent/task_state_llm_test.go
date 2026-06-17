package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

type llmExtractFake struct {
	completionQueue []any
	calls           []provider.ChatRequest
	pos             int
}

func (f *llmExtractFake) ChatCompletion(_ context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	f.calls = append(f.calls, req)
	if f.pos >= len(f.completionQueue) {
		return nil, fmt.Errorf("no queued completion")
	}
	item := f.completionQueue[f.pos]
	f.pos++
	switch v := item.(type) {
	case *provider.ChatResponse:
		return v, nil
	case error:
		return nil, v
	default:
		return nil, fmt.Errorf("unexpected type: %T", v)
	}
}

func (f *llmExtractFake) ChatCompletionStream(context.Context, provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return nil, fmt.Errorf("stream not implemented")
}

func TestUpdateTaskStateLLMExtract(t *testing.T) {
	jsonResp := `{"schema_version":1,"goal":"Book Paris","constraints":{"seat":"aisle"}}`
	fake := &llmExtractFake{completionQueue: []any{&provider.ChatResponse{Text: jsonResp}}}
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "gpt-4o", MaxRetries: 0})
	llmCore.SetClientForModel("gpt-4o", fake)
	chat := client.NewChatClient(llmCore, tool.NewToolExecutor(), nil)

	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	enabled := true
	update := "llm_extract"
	a := New(chat, tm, NopHooks(), Config{
		AgentPolicy: config.AgentConfig{
			Handoff: config.HandoffConfig{
				StateEnabled: &enabled,
				StateUpdate:  update,
			},
		},
	})

	const tapeName = "llm-extract"
	a.initTaskStateFromPrompt(tapeName, "Book Paris")
	_ = tm.Store.Append(tapeName, tape.NewMessageEntry(map[string]any{
		"role": "user", "content": "I need an aisle seat please",
	}))

	a.updateTaskStateAfterToolRound(context.Background(), tapeName, 1)

	st := a.latestTaskState(tapeName)
	if st == nil {
		t.Fatal("expected task state")
	}
	if st.Source != "llm_extract" {
		t.Fatalf("source = %q", st.Source)
	}
	if st.Constraints["seat"] != "aisle" {
		t.Fatalf("constraints = %v", st.Constraints)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(fake.calls))
	}
}
