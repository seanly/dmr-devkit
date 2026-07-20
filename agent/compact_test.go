package agent

import (
	"context"
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
	summarize := a.buildSummarizer("tape1")

	summary, _, err := summarize(context.Background(), []map[string]any{
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
	summarize := a.buildSummarizer("tape1")

	summary, _, err := summarize(context.Background(), []map[string]any{
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
	summarize := a.buildSummarizer("tape1")

	_, _, err := summarize(context.Background(), []map[string]any{
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

func TestCompact_QualityFallbackSkipsPoorSummary(t *testing.T) {
	// Summary text is only 14 chars, so evaluateCompactSummary rates it Poor (< 20).
	// QualityFallback is enabled, so the summary should be skipped and an anchor with
	// fallback_keep_before should be written instead.
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: "<summary>unrelated text</summary>", Usage: &provider.Usage{TotalTokens: 10}},
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
			Context: config.ContextConfig{
				QualityFallback:           true,
				QualityFallbackKeepBefore: 8,
				KeepBeforeAnchor:          2,
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

	_ = tm.AppendEntry("qf-tape", tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	_ = tm.AppendEntry("qf-tape", tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "hi"}))

	if _, err := a.CompactTape(context.Background(), "qf-tape"); err != nil {
		t.Fatalf("CompactTape failed: %v", err)
	}

	entries, _ := store.FetchAll("qf-tape", nil)
	var foundSummary, foundAnchor bool
	var keepBefore int
	for _, e := range entries {
		if e.Kind == "compact_summary" {
			foundSummary = true
		}
		if e.Kind == "anchor" {
			foundAnchor = true
			if state, ok := e.Payload["state"].(map[string]any); ok {
				if fb, ok := state["fallback_keep_before"].(int); ok {
					keepBefore = fb
				}
			}
		}
	}
	if foundSummary {
		t.Error("expected poor summary to be skipped, but compact_summary was written")
	}
	if !foundAnchor {
		t.Fatal("expected anchor to be written")
	}
	if keepBefore != 8 {
		t.Errorf("fallback_keep_before = %d, want 8", keepBefore)
	}
}

func TestCompact_RecordsMetrics(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: "<summary>the summary</summary>", Usage: &provider.Usage{TotalTokens: 10}},
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
			Context:          config.ContextConfig{Strategy: config.CompactStrategyCollapse},
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

	_ = tm.AppendEntry("metrics-tape", tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	if _, err := a.CompactTape(context.Background(), "metrics-tape"); err != nil {
		t.Fatalf("CompactTape failed: %v", err)
	}

	entries, _ := store.FetchAll("metrics-tape", nil)
	var found bool
	for _, e := range entries {
		if e.Kind != "event" {
			continue
		}
		name, _ := e.Payload["name"].(string)
		if name != "loop:compact" {
			continue
		}
		found = true
		data, _ := e.Payload["data"].(map[string]any)
		if data["trigger_reason"] != "manual" {
			t.Errorf("trigger_reason = %v, want manual", data["trigger_reason"])
		}
		if data["strategy"] != "collapse" {
			t.Errorf("strategy = %v, want collapse", data["strategy"])
		}
		if _, ok := data["original_tokens"].(int); !ok {
			t.Errorf("original_tokens missing or wrong type: %T", data["original_tokens"])
		}
		if _, ok := data["optimized_tokens"].(int); !ok {
			t.Errorf("optimized_tokens missing or wrong type: %T", data["optimized_tokens"])
		}
	}
	if !found {
		t.Fatal("expected loop:compact event with metrics")
	}
}

func TestCompact_HeuristicQuality(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: "<summary>we are refactoring the handoff pipeline</summary>", Usage: &provider.Usage{TotalTokens: 10}},
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
			Context: config.ContextConfig{},
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

	_ = tm.AppendEntry("heuristic-quality-tape", tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	_ = tm.AppendEntry("heuristic-quality-tape", tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "hi"}))

	if _, err := a.CompactTape(context.Background(), "heuristic-quality-tape"); err != nil {
		t.Fatalf("CompactTape failed: %v", err)
	}

	// After lean compact: only 1 LLM call (summarizer), no separate judge call.
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 LLM call (summarizer only), got %d", len(fake.calls))
	}

	entries, _ := store.FetchAll("heuristic-quality-tape", nil)
	var found bool
	for _, e := range entries {
		if e.Kind != "event" {
			continue
		}
		name, _ := e.Payload["name"].(string)
		if name != "loop:compact" {
			continue
		}
		found = true
		data, _ := e.Payload["data"].(map[string]any)
		quality, ok := data["quality"].(string)
		if !ok || quality != "fair" {
			t.Errorf("quality = %v, want fair", data["quality"])
		}
	}
	if !found {
		t.Fatal("expected loop:compact event")
	}
}

func TestCompact_SessionMemorySkipsLLMCall(t *testing.T) {
	// The fake client errors if called; if session memory produces a usable
	// summary, the LLM path must never be reached.
	fake := &summarizerFakeClient{completionQueue: []any{
		fmt.Errorf("LLM should not be called when session memory is sufficient"),
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
			Handoff:          config.HandoffConfig{CompactAfterState: true},
			Context: config.ContextConfig{
				SessionMemory:        true,
				SessionMemoryFallback: false, // do not fall back to LLM
			},
		},
		Models: []config.ModelConfig{
			{Name: "test-model", Model: "test-model", Default: true, MaxToken: 100000, HandoffThreshold: 0.8},
		},
	})

	// Seed the tape with a couple of messages so tape.Compact has content to anchor.
	_ = tm.AppendEntry("sm-tape", tape.NewMessageEntry(map[string]any{"role": "user", "content": "refactor the compact pipeline"}))
	_ = tm.AppendEntry("sm-tape", tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "on it"}))

	// Populate session memory with enough rich content.
	mem := a.sessionMemoryForTape("sm-tape")
	mem.AppendSegment(memorySegment{Step: 1, Type: segUserIntent, Content: "Refactor the compact pipeline to use a coordinator"})
	mem.AppendSegment(memorySegment{Step: 2, Type: segFileChange, Content: "edit_file: agent/loop.go"})
	mem.AppendSegment(memorySegment{Step: 3, Type: segToolResult, Content: "read_file: agent/compact.go returned 322 lines"})
	mem.AppendSegment(memorySegment{Step: 4, Type: segError, Content: "build failed: undefined applyProgressiveTrim"})
	mem.AppendSegment(memorySegment{Step: 5, Type: segDecision, Content: "Decided to route all triggers through the coordinator"})

	if _, err := a.CompactTape(context.Background(), "sm-tape"); err != nil {
		t.Fatalf("CompactTape failed: %v", err)
	}

	if len(fake.calls) != 0 {
		t.Fatalf("expected zero LLM calls, got %d", len(fake.calls))
	}

	entries, _ := store.FetchAll("sm-tape", nil)
	var summary string
	for _, e := range entries {
		if e.Kind == "compact_summary" {
			if c, ok := e.Payload["content"].(string); ok {
				summary = c
			}
		}
	}
	if !strings.Contains(summary, "coordinator") {
		t.Errorf("session-memory summary should mention the coordinator, got:\n%s", summary)
	}
}

func TestWriteCompactEntries_PostCompactRebuildWithAnchorMetadata(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := newSummarizerTestAgent(&summarizerFakeClient{})
	a.tape = tm

	prevUUID := "550e8400-e29b-41d4-a716-446655440000"
	if _, err := tm.Handoff("meta-tape", "session/start", map[string]any{tape.StateKeyAnchorUUID: prevUUID}); err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}
	_ = tm.AppendEntry("meta-tape", tape.NewMessageEntry(map[string]any{"role": "user", "content": "find bugs"}))
	_ = tm.AppendEntry("meta-tape", tape.NewToolCallEntry([]map[string]any{
		{"id": "1", "type": "function", "function": map[string]any{"name": "tapeSearch", "arguments": `{"query":"bug"}`}},
	}))

	if _, err := a.writeCompactEntries(context.Background(), "meta-tape", "auto:proactive:test", "proactive",
		"summary about bugs", CompactQualityGood, CompactSummaryStats{Strategy: "test"}); err != nil {
		t.Fatalf("writeCompactEntries failed: %v", err)
	}

	entries, _ := store.FetchAll("meta-tape", nil)
	var anchorState map[string]any
	var rebuildContent string
	for _, e := range entries {
		if e.Kind == "anchor" && e.Payload["name"] == "auto:proactive:test" {
			anchorState = tape.AnchorState(e)
		}
		if e.Kind == "system" {
			if c, ok := e.Payload["content"].(string); ok && strings.Contains(c, "[Post-Compact Context Rebuild]") {
				rebuildContent = c
			}
		}
	}
	if anchorState == nil {
		t.Fatal("expected compact anchor")
	}
	if anchorState["previous_anchor_uuid"] != prevUUID {
		t.Errorf("anchor previous_anchor_uuid = %v, want %s", anchorState["previous_anchor_uuid"], prevUUID)
	}
	if rebuildContent == "" {
		t.Fatal("expected post-compact rebuild system entry")
	}
	if !strings.Contains(rebuildContent, "between_uuid=") {
		t.Errorf("rebuild should include between_uuid guidance, got:\n%s", rebuildContent)
	}
	if !strings.Contains(rebuildContent, "tapeSearch") {
		t.Errorf("rebuild should mention archived tool hints, got:\n%s", rebuildContent)
	}
}
