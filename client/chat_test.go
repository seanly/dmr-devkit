package client

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// FakeClient implements core.ChatProvider for testing.
type fakeClient struct {
	completionQueue []any
	streamQueue     []any
	calls           []provider.ChatRequest
	pos             int
	streamPos       int
}

func (f *fakeClient) ChatCompletion(_ context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
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

func (f *fakeClient) ChatCompletionStream(_ context.Context, req provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	f.calls = append(f.calls, req)
	if f.streamPos >= len(f.streamQueue) {
		return nil, fmt.Errorf("no queued stream")
	}
	item := f.streamQueue[f.streamPos]
	f.streamPos++
	switch v := item.(type) {
	case <-chan provider.StreamChunk:
		return v, nil
	case error:
		return nil, v
	default:
		return nil, fmt.Errorf("unexpected type: %T", v)
	}
}

func newTestCore(fake *fakeClient) *core.LLMCore {
	c := core.NewLLMCore(core.LLMCoreConfig{Model: "gpt-4o", MaxRetries: 0})
	c.SetClientForModel("gpt-4o", fake)
	return c
}

func newTestChatClient(fake *fakeClient) *ChatClient {
	return NewChatClient(newTestCore(fake), tool.NewToolExecutor(), nil)
}

func resp(text string) *provider.ChatResponse {
	return &provider.ChatResponse{Text: text, Usage: &provider.Usage{TotalTokens: 5}}
}

func respWithToolCalls(calls ...provider.ToolCall) *provider.ChatResponse {
	return &provider.ChatResponse{ToolCalls: calls, Usage: &provider.Usage{TotalTokens: 10}}
}

func respWithReasoning(text, reasoning string) *provider.ChatResponse {
	return &provider.ChatResponse{Text: text, Reasoning: reasoning, Usage: &provider.Usage{TotalTokens: 5}}
}

func TestChatReturnsText(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("hello")}}
	cc := newTestChatClient(fake)

	result, err := cc.Chat(context.Background(), ChatOpts{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("result = %q", result)
	}
}

func TestChatRawReturnsTextAndReasoning(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{respWithReasoning("hello", "thinking")}}
	cc := newTestChatClient(fake)

	result, err := cc.ChatRaw(context.Background(), ChatOpts{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello" {
		t.Errorf("text = %q", result.Text)
	}
	if result.Reasoning != "thinking" {
		t.Errorf("reasoning = %q", result.Reasoning)
	}
}

func TestChatWithSystemPrompt(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("ok")}}
	cc := newTestChatClient(fake)

	_, err := cc.Chat(context.Background(), ChatOpts{
		Prompt:       "hi",
		SystemPrompt: "be helpful",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 {
		t.Fatal("expected 1 call")
	}
	msgs := fake.calls[0].Messages
	if msgs[0].Role != "system" || msgs[0].Content != "be helpful" {
		t.Error("system prompt not at index 0")
	}
}

func TestChatWithMessages(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("answer")}}
	cc := newTestChatClient(fake)

	_, err := cc.Chat(context.Background(), ChatOpts{
		Messages: []map[string]any{
			{"role": "user", "content": "q1"},
			{"role": "assistant", "content": "a1"},
			{"role": "user", "content": "q2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls[0].Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(fake.calls[0].Messages))
	}
}

func TestCollapseSystemMessagesMergesDistinctBodies(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "first"},
		{"role": "system", "content": "second"},
		{"role": "user", "content": "hi"},
	}
	out := collapseSystemMessages(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if out[0]["role"] != "system" || out[0]["content"] != "first\n\nsecond" {
		t.Errorf("system = %#v", out[0])
	}
	if out[1]["role"] != "user" {
		t.Errorf("want user after system, got %#v", out[1])
	}
}

func TestCollapseSystemMessagesDedupesAdjacentIdentical(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "same"},
		{"role": "system", "content": "same"},
		{"role": "user", "content": "hi"},
	}
	out := collapseSystemMessages(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if out[0]["content"] != "same" {
		t.Errorf("content = %q", out[0]["content"])
	}
}

func TestChatMergesPrependedAndTapeSystemWhenIdentical(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("ok")}}
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	_ = store.Append("t1", tape.NewSystemEntry("be helpful"))
	_ = store.Append("t1", tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))

	cc := NewChatClient(newTestCore(fake), tool.NewToolExecutor(), tm)
	_, err := cc.Chat(context.Background(), ChatOpts{
		Tape:         "t1",
		Context:      tape.NewNoAnchorContext(),
		SystemPrompt: "be helpful",
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := fake.calls[0].Messages
	var systemCount int
	for _, m := range msgs {
		if m.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Fatalf("expected 1 system message, got %d (total %d)", systemCount, len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "be helpful") {
		t.Errorf("system content = %q", msgs[0].Content)
	}
}

func TestChatOptsMessagesMergesDuplicateSystem(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("ok")}}
	cc := newTestChatClient(fake)

	_, err := cc.Chat(context.Background(), ChatOpts{
		Messages: []map[string]any{
			{"role": "system", "content": "x"},
			{"role": "system", "content": "x"},
			{"role": "user", "content": "q"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := fake.calls[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "x" {
		t.Errorf("first = %+v", msgs[0])
	}
	if msgs[1].Role != "user" {
		t.Errorf("second = %+v", msgs[1])
	}
}

func TestToolCallsReturnsCallList(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}`},
		}),
	}}
	cc := newTestChatClient(fake)

	echoTool := &tool.Tool{Spec: tool.ToolSpec{Name: "echo"}, Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
		return args["text"], nil
	}}

	calls, err := cc.ToolCalls(context.Background(), ChatOpts{
		Prompt: "call echo",
		Tools:  []*tool.Tool{echoTool},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Function.Name != "echo" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestRunToolsExecutesAndReturnsResult(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}`},
		}),
	}}
	cc := newTestChatClient(fake)

	echoTool := &tool.Tool{Spec: tool.ToolSpec{Name: "echo"}, Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
		text, _ := args["text"].(string)
		return "ECHO:" + text, nil
	}}

	result, err := cc.RunTools(context.Background(), ChatOpts{
		Prompt: "call echo",
		Tools:  []*tool.Tool{echoTool},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "tools" {
		t.Errorf("kind = %q", result.Kind)
	}
	want := "[LIVE DATA from echo]\nECHO:tokyo"
	if len(result.ToolResults) != 1 || result.ToolResults[0] != want {
		t.Errorf("results = %v, want %q", result.ToolResults, want)
	}
}

func TestRunToolsSplitsConcatenatedArgs(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}{"text":"osaka"}`},
		}),
	}}
	cc := newTestChatClient(fake)

	echoTool := &tool.Tool{Spec: tool.ToolSpec{Name: "echo"}, Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
		text, _ := args["text"].(string)
		return text + "!", nil
	}}

	result, err := cc.RunTools(context.Background(), ChatOpts{
		Prompt: "call echo twice",
		Tools:  []*tool.Tool{echoTool},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ID != "call_1" || result.ToolCalls[1].ID != "call_1__2" {
		t.Errorf("IDs = %q, %q", result.ToolCalls[0].ID, result.ToolCalls[1].ID)
	}
	if len(result.ToolResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.ToolResults))
	}
	want0 := "[LIVE DATA from echo]\ntokyo!"
	want1 := "[LIVE DATA from echo]\nosaka!"
	if result.ToolResults[0] != want0 || result.ToolResults[1] != want1 {
		t.Errorf("results = %v, want %q, %q", result.ToolResults, want0, want1)
	}
}

func TestStreamReturnsTextChannel(t *testing.T) {
	ch := make(chan provider.StreamChunk, 3)
	ch <- provider.StreamChunk{Text: "hello "}
	ch <- provider.StreamChunk{Text: "world"}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 7}}
	close(ch)

	fake := &fakeClient{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	cc := newTestChatClient(fake)

	textCh, state, err := cc.Stream(context.Background(), ChatOpts{Prompt: "say hello"})
	if err != nil {
		t.Fatal(err)
	}

	var text string
	for s := range textCh {
		text += s
	}
	if text != "hello world" {
		t.Errorf("text = %q", text)
	}
	if state.Usage == nil || state.Usage["total_tokens"] != 7 {
		t.Errorf("usage = %v", state.Usage)
	}
	if state.Error != nil {
		t.Errorf("unexpected error: %v", state.Error)
	}
}

func TestStreamCollectsUsage(t *testing.T) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Text: "ok"}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 42}}
	close(ch)

	fake := &fakeClient{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	cc := newTestChatClient(fake)

	textCh, state, err := cc.Stream(context.Background(), ChatOpts{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for range textCh {
	}
	if state.Usage["total_tokens"] != 42 {
		t.Errorf("usage = %v", state.Usage)
	}
}

func TestStreamEventsAllKinds(t *testing.T) {
	ch := make(chan provider.StreamChunk, 4)
	ch <- provider.StreamChunk{Text: "Checking "}
	ch <- provider.StreamChunk{ToolCalls: []provider.ToolCall{
		{ID: "call_1", Type: "function", Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}`}},
	}}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 12}}
	close(ch)

	fake := &fakeClient{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	cc := newTestChatClient(fake)

	echoTool := &tool.Tool{Spec: tool.ToolSpec{Name: "echo"}, Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
		text, _ := args["text"].(string)
		return text + "!", nil
	}}

	eventCh, state, err := cc.StreamEvents(context.Background(), ChatOpts{
		Prompt: "call echo",
		Tools:  []*tool.Tool{echoTool},
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []core.StreamEvent
	for e := range eventCh {
		events = append(events, e)
	}

	kinds := make([]string, len(events))
	for i, e := range events {
		kinds[i] = string(e.Kind)
	}

	hasText, hasToolCall, hasToolResult, hasUsage, hasFinal := false, false, false, false, false
	for _, k := range kinds {
		switch k {
		case "text":
			hasText = true
		case "tool_call":
			hasToolCall = true
		case "tool_result":
			hasToolResult = true
		case "usage":
			hasUsage = true
		case "final":
			hasFinal = true
		}
	}
	if !hasText {
		t.Error("missing text event")
	}
	if !hasToolCall {
		t.Error("missing tool_call event")
	}
	if !hasToolResult {
		t.Error("missing tool_result event")
	}
	if !hasUsage {
		t.Error("missing usage event")
	}
	if !hasFinal {
		t.Error("missing final event")
	}
	if kinds[len(kinds)-1] != "final" {
		t.Error("last event should be final")
	}

	// Check tool result value
	for _, e := range events {
		if e.Kind == core.StreamToolResult {
			want := "[LIVE DATA from echo]\ntokyo!"
			if e.Data["result"] != want {
				t.Errorf("tool result = %v, want %q", e.Data["result"], want)
			}
		}
	}

	if state.Error != nil {
		t.Errorf("unexpected error: %v", state.Error)
	}
}

func TestStreamEventsMergesToolDeltas(t *testing.T) {
	ch := make(chan provider.StreamChunk, 3)
	ch <- provider.StreamChunk{ToolCalls: []provider.ToolCall{
		{ID: "call_1", Type: "function", Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"to`}},
	}}
	ch <- provider.StreamChunk{ToolCalls: []provider.ToolCall{
		{Type: "function", Function: provider.ToolCallFunction{Arguments: `kyo"}`}},
	}}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 9}}
	close(ch)

	fake := &fakeClient{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	cc := newTestChatClient(fake)

	echoTool := &tool.Tool{Spec: tool.ToolSpec{Name: "echo"}, Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
		text, _ := args["text"].(string)
		return text + "!", nil
	}}

	eventCh, _, err := cc.StreamEvents(context.Background(), ChatOpts{
		Prompt: "call echo",
		Tools:  []*tool.Tool{echoTool},
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []core.StreamEvent
	for e := range eventCh {
		events = append(events, e)
	}

	toolCalls := make([]core.StreamEvent, 0)
	for _, e := range events {
		if e.Kind == core.StreamToolCall {
			toolCalls = append(toolCalls, e)
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	callMap := toolCalls[0].Data["call"].(map[string]any)
	fn := callMap["function"].(map[string]any)
	if fn["name"] != "echo" {
		t.Errorf("name = %v", fn["name"])
	}
	if fn["arguments"] != `{"text":"tokyo"}` {
		t.Errorf("arguments = %v", fn["arguments"])
	}
}

func TestStreamEventsSplitsConcatenatedArgs(t *testing.T) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{ToolCalls: []provider.ToolCall{
		{ID: "call_1", Type: "function", Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}{"text":"osaka"}`}},
	}}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 8}}
	close(ch)

	fake := &fakeClient{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	cc := newTestChatClient(fake)

	echoTool := &tool.Tool{Spec: tool.ToolSpec{Name: "echo"}, Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
		text, _ := args["text"].(string)
		return text, nil
	}}

	eventCh, _, err := cc.StreamEvents(context.Background(), ChatOpts{
		Prompt: "call echo",
		Tools:  []*tool.Tool{echoTool},
	})
	if err != nil {
		t.Fatal(err)
	}

	var toolCallEvents []core.StreamEvent
	for e := range eventCh {
		if e.Kind == core.StreamToolCall {
			toolCallEvents = append(toolCallEvents, e)
		}
	}
	if len(toolCallEvents) != 2 {
		t.Fatalf("expected 2 tool_call events, got %d", len(toolCallEvents))
	}
}

func TestChatRecordsTape(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("step one"), resp("step two")}}
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	cc := NewChatClient(newTestCore(fake), tool.NewToolExecutor(), tm)

	// Handoff first
	if _, err := tm.Handoff("ops", "incident_42", map[string]any{"owner": "tier1"}); err != nil {
		t.Fatal(err)
	}

	first, err := cc.Chat(context.Background(), ChatOpts{
		Prompt:  "Investigate DB timeout",
		Tape:    "ops",
		Context: tape.NewLastAnchorContext(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != "step one" {
		t.Errorf("first = %q", first)
	}

	entries, _ := store.FetchAll("ops", nil)
	hasEvent := false
	for _, e := range entries {
		if e.Kind == "event" {
			data, _ := e.Payload["data"].(map[string]any)
			if data != nil && data["status"] == "ok" {
				hasEvent = true
			}
		}
	}
	if !hasEvent {
		t.Error("expected run event in tape")
	}
}

func TestMaxTokensPassthrough(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("ok")}}
	cc := newTestChatClient(fake)

	_, err := cc.Chat(context.Background(), ChatOpts{Prompt: "hi", MaxTokens: 42})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls[0].MaxTokens != 42 {
		t.Errorf("MaxTokens = %d", fake.calls[0].MaxTokens)
	}
}

func TestExtraHeadersPassthrough(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("ok")}}
	cc := newTestChatClient(fake)

	_, err := cc.Chat(context.Background(), ChatOpts{
		Prompt:       "hi",
		ExtraHeaders: map[string]string{"X-Title": "Republic"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls[0].ExtraHeaders["X-Title"] != "Republic" {
		t.Error("extra headers not passed through")
	}
}

func TestRunToolsTextResultWhenNoToolCalls(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("just text")}}
	cc := newTestChatClient(fake)

	result, err := cc.RunTools(context.Background(), ChatOpts{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "text" {
		t.Errorf("kind = %q", result.Kind)
	}
	if result.Text != "just text" {
		t.Errorf("text = %q", result.Text)
	}
}

func TestStreamStateErrorCapture(t *testing.T) {
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Err: fmt.Errorf("stream error")}
	close(ch)

	fake := &fakeClient{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	cc := newTestChatClient(fake)

	textCh, state, err := cc.Stream(context.Background(), ChatOpts{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	for range textCh {
	}
	if state.Error == nil {
		t.Error("expected error in stream state")
	}
}

func TestSplitConcatenatedJSON(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{`{"a":1}`, 1},
		{`{"a":1}{"b":2}`, 2},
		{`{"a":1}{"b":2}{"c":3}`, 3},
		{`not json`, 1},
		{``, 1},
	}
	for _, tc := range cases {
		parts := splitConcatenatedJSON(tc.input)
		if len(parts) != tc.want {
			t.Errorf("splitConcatenatedJSON(%q) = %d parts, want %d", tc.input, len(parts), tc.want)
		}
	}
}

func TestToolCallAssemblerMergesChunks(t *testing.T) {
	a := newToolCallAssembler()
	a.addDelta(provider.ToolCall{
		ID: "call_1", Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"to`},
	})
	a.addDelta(provider.ToolCall{
		Function: provider.ToolCallFunction{Arguments: `kyo"}`},
	})

	calls := a.complete()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "echo" {
		t.Errorf("name = %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"text":"tokyo"}` {
		t.Errorf("args = %q", calls[0].Function.Arguments)
	}
}

func TestThinkFilterBasic(t *testing.T) {
	tf := &thinkFilter{}
	out := tf.Feed("<think>reasoning</think>visible")
	out += tf.Flush()
	if out != "visible" {
		t.Errorf("got %q, want %q", out, "visible")
	}
}

func TestThinkFilterAcrossChunks(t *testing.T) {
	tf := &thinkFilter{}
	var out string
	// Tag split across multiple chunks
	out += tf.Feed("<thi")
	out += tf.Feed("nk>hidden rea")
	out += tf.Feed("soning</thi")
	out += tf.Feed("nk>hello ")
	out += tf.Feed("world")
	out += tf.Flush()
	if out != "hello world" {
		t.Errorf("got %q, want %q", out, "hello world")
	}
}

func TestThinkFilterNoTags(t *testing.T) {
	tf := &thinkFilter{}
	out := tf.Feed("just normal text")
	out += tf.Flush()
	if out != "just normal text" {
		t.Errorf("got %q, want %q", out, "just normal text")
	}
}

func TestThinkFilterMultipleBlocks(t *testing.T) {
	tf := &thinkFilter{}
	out := tf.Feed("<think>first</think>A<think>second</think>B")
	out += tf.Flush()
	if out != "AB" {
		t.Errorf("got %q, want %q", out, "AB")
	}
}

func TestThinkFilterPartialOpenTagNotMatch(t *testing.T) {
	tf := &thinkFilter{}
	// "<thx" starts like "<think>" but diverges — should be flushed as text
	out := tf.Feed("<thx>hello")
	out += tf.Flush()
	if out != "<thx>hello" {
		t.Errorf("got %q, want %q", out, "<thx>hello")
	}
}

func TestStreamStripsThinkTags(t *testing.T) {
	ch := make(chan provider.StreamChunk, 5)
	ch <- provider.StreamChunk{Text: "<thi"}
	ch <- provider.StreamChunk{Text: "nk>internal reasoning</"}
	ch <- provider.StreamChunk{Text: "think>visible "}
	ch <- provider.StreamChunk{Text: "reply"}
	close(ch)

	fake := &fakeClient{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	cc := newTestChatClient(fake)

	textCh, _, err := cc.Stream(context.Background(), ChatOpts{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}

	var text string
	for s := range textCh {
		text += s
	}
	if text != "visible reply" {
		t.Errorf("text = %q, want %q", text, "visible reply")
	}
}

func TestPrepareStripImageParts(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{resp("ok")}}
	cc := newTestChatClient(fake)

	_, err := cc.Chat(context.Background(), ChatOpts{
		Prompt: "follow up",
		Messages: []map[string]any{
			{
				"role":    "user",
				"content": "what is this?",
				"parts": []any{
					map[string]any{"type": "text", "text": "what is this?"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
		StripImageParts: true,
		PromptParts: []provider.ContentPart{
			provider.ImagePart{URL: "data:image/png;base64,xyz"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.calls))
	}
	for _, m := range fake.calls[0].Messages {
		for _, p := range m.Parts {
			if _, ok := p.(provider.ImagePart); ok {
				t.Fatalf("image part leaked to provider: %#v", m)
			}
		}
	}
}
