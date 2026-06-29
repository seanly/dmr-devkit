package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tool"
)

type summarizerFakeClient struct {
	completionQueue []any
	calls           []provider.ChatRequest
	pos             int
}

func (f *summarizerFakeClient) ChatCompletion(_ context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
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

func (f *summarizerFakeClient) ChatCompletionStream(_ context.Context, _ provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func newSummarizerTestAgent(fake *summarizerFakeClient) *Agent {
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "test-model", MaxRetries: 0})
	llmCore.SetClientForModel("test-model", fake)
	chat := client.NewChatClient(llmCore, tool.NewToolExecutor(), nil)
	return New(chat, nil, nil, Config{
		AgentPolicy: config.AgentConfig{
			MaxToken:         100000,
			HandoffThreshold: 0.8,
			Scaffolding: config.ScaffoldingConfig{
				Profile: "standard",
			},
		},
		Models: []config.ModelConfig{
			{
				Name:             "test-model",
				Model:            "test-model",
				Default:          true,
				MaxToken:         100000,
				HandoffThreshold: 0.8,
			},
		},
	})
}

func TestBuildSummarizer_ExtractsSummaryTag(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: "<summary>the summary</summary>", Usage: &provider.Usage{TotalTokens: 10}},
	}}
	a := newSummarizerTestAgent(fake)
	summarize := a.buildSummarizer("")

	summary, err := summarize(context.Background(), []map[string]any{
		{"role": "user", "content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "the summary" {
		t.Errorf("summary = %q", summary)
	}
}

func TestBuildSummarizer_FallsBackToReasoningWhenTextEmpty(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{
			Text:      "",
			Reasoning: "<summary>reasoning summary</summary>",
			Usage:     &provider.Usage{TotalTokens: 10},
		},
	}}
	a := newSummarizerTestAgent(fake)
	summarize := a.buildSummarizer("")

	summary, err := summarize(context.Background(), []map[string]any{
		{"role": "user", "content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "reasoning summary" {
		t.Errorf("summary = %q", summary)
	}
}

func TestBuildSummarizer_ReturnsErrorWhenSummaryEmpty(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: "", Reasoning: "", Usage: &provider.Usage{TotalTokens: 10}},
	}}
	a := newSummarizerTestAgent(fake)
	summarize := a.buildSummarizer("")

	_, err := summarize(context.Background(), []map[string]any{
		{"role": "user", "content": "hello"},
	})
	if err == nil {
		t.Fatal("expected error for empty summary")
	}
}
