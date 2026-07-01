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
	summarize := a.buildSummarizer("tape1", "")

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
	summarize := a.buildSummarizer("tape1", "")

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
	summarize := a.buildSummarizer("tape1", "")

	_, err := summarize(context.Background(), []map[string]any{
		{"role": "user", "content": "hello"},
	})
	if err == nil {
		t.Fatal("expected error for empty summary")
	}
}

func TestCompact_PassesHandoffConfigVersion(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: "<summary>configured version summary</summary>", Usage: &provider.Usage{TotalTokens: 10}},
	}}

	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	llmCore := core.NewLLMCore(core.LLMCoreConfig{Model: "test-model", MaxRetries: 0})
	llmCore.SetClientForModel("test-model", fake)
	chat := client.NewChatClient(llmCore, tool.NewToolExecutor(), tm)

	a := New(chat, tm, nil, Config{
		AgentPolicy: config.AgentConfig{
			MaxToken:         100000,
			HandoffThreshold: 0.8,
			Scaffolding:      config.ScaffoldingConfig{Profile: "standard"},
			Handoff:          config.HandoffConfig{CompactAfterState: true, CompactSummaryVersion: 2},
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

	_ = tm.AppendEntry("versioned-tape", tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	_ = tm.AppendEntry("versioned-tape", tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "hi"}))

	if _, err := a.CompactTape(context.Background(), "versioned-tape"); err != nil {
		t.Fatalf("CompactTape failed: %v", err)
	}

	entries, _ := store.FetchAll("versioned-tape", nil)
	var found bool
	for _, e := range entries {
		if e.Kind != "compact_summary" {
			continue
		}
		found = true
		if got := e.Payload["schema_version"]; got != 2 {
			t.Errorf("payload schema_version = %v, want 2", got)
		}
		if got := e.Meta["schema_version"]; got != 2 {
			t.Errorf("meta schema_version = %v, want 2", got)
		}
	}
	if !found {
		t.Fatal("expected compact_summary entry in tape")
	}
}
