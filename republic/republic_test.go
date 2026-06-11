package republic

import (
	"context"
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// fakeProvider implements core.ChatProvider for integration testing.
type fakeProvider struct {
	completionQueue []any
	streamQueue     []any
	calls           []provider.ChatRequest
	pos             int
	streamPos       int
}

func (f *fakeProvider) ChatCompletion(_ context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	f.calls = append(f.calls, req)
	if f.pos >= len(f.completionQueue) {
		return nil, fmt.Errorf("no queued completion for call %d", f.pos)
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

func (f *fakeProvider) ChatCompletionStream(_ context.Context, req provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	f.calls = append(f.calls, req)
	if f.streamPos >= len(f.streamQueue) {
		return nil, fmt.Errorf("no queued stream for call %d", f.streamPos)
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

func resp(text string) *provider.ChatResponse {
	return &provider.ChatResponse{Text: text, Usage: &provider.Usage{TotalTokens: 5}}
}

func respToolCalls(calls ...provider.ToolCall) *provider.ChatResponse {
	return &provider.ChatResponse{ToolCalls: calls, Usage: &provider.Usage{TotalTokens: 10}}
}

type httpErr struct {
	msg  string
	code int
}

func (e *httpErr) Error() string   { return e.msg }
func (e *httpErr) StatusCode() int { return e.code }

func newTestLLM(fake *fakeProvider) *LLM {
	llm := New(Config{Model: "gpt-4o", APIKey: "test", MaxRetries: 0})
	llm.llmCore.SetClientForModel("gpt-4o", fake)
	return llm
}

func newTestLLMWithRetries(fake *fakeProvider, retries int, classifier func(error) core.ErrorKind) *LLM {
	llm := New(Config{Model: "gpt-4o", APIKey: "test", MaxRetries: retries, ErrorClassifier: classifier})
	llm.llmCore.SetClientForModel("gpt-4o", fake)
	return llm
}

// --- Tests ---

func TestNewDefault(t *testing.T) {
	llm := New(Config{Model: "gpt-4o", APIKey: "test"})
	if llm == nil {
		t.Fatal("expected non-nil LLM")
	}
	if llm.llmCore.Model() != "gpt-4o" {
		t.Errorf("model = %q", llm.llmCore.Model())
	}
	if llm.llmCore.MaxRetries() != 3 {
		t.Errorf("max_retries = %d, want 3", llm.llmCore.MaxRetries())
	}
}

func TestNewCustomConfig(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	llm := New(Config{
		Model:          "gpt-4o-mini",
		APIKey:         "custom-key",
		BaseURL:        "https://custom.api/v1",
		FallbackModels: []string{"fallback-model"},
		MaxRetries:     5,
		Verbose:        2,
		TapeStore:      store,
	})
	if llm.llmCore.Model() != "gpt-4o-mini" {
		t.Error("model mismatch")
	}
	if llm.llmCore.MaxRetries() != 5 {
		t.Errorf("max_retries = %d", llm.llmCore.MaxRetries())
	}
	if llm.llmCore.Verbose() != 2 {
		t.Errorf("verbose = %d", llm.llmCore.Verbose())
	}
}

func TestChatEndToEnd(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("hello world")}}
	llm := newTestLLM(fake)

	result, err := llm.Chat(context.Background(), "Say hello")
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello world" {
		t.Errorf("result = %q", result)
	}
}

func TestChatWithRetry(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{
		fmt.Errorf("temporary failure"),
		resp("recovered"),
	}}
	llm := newTestLLMWithRetries(fake, 2, func(_ error) core.ErrorKind { return core.ErrTemporary })

	result, err := llm.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if result != "recovered" {
		t.Errorf("result = %q", result)
	}
	if len(fake.calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(fake.calls))
	}
}

func TestChatWithFallback(t *testing.T) {
	primary := &fakeProvider{completionQueue: []any{fmt.Errorf("primary down")}}
	fallback := &fakeProvider{completionQueue: []any{resp("fallback ok")}}

	llm := New(Config{
		Model:           "gpt-4o",
		APIKey:          "test",
		FallbackModels:  []string{"fallback-model"},
		MaxRetries:      0,
		ErrorClassifier: func(_ error) core.ErrorKind { return core.ErrTemporary },
	})
	llm.llmCore.SetClientForModel("gpt-4o", primary)
	llm.llmCore.SetClientForModel("fallback-model", fallback)

	result, err := llm.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if result != "fallback ok" {
		t.Errorf("result = %q", result)
	}
}

func TestToolCallsEndToEnd(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{
		respToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}`},
		}),
	}}
	llm := newTestLLM(fake)

	echoTool := &tool.Tool{
		Spec: tool.ToolSpec{Name: "echo"},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			return args["text"], nil
		},
	}

	calls, err := llm.ToolCalls(context.Background(), "call echo", WithTools(echoTool))
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Function.Name != "echo" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestRunToolsEndToEnd(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{
		respToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}`},
		}),
	}}
	llm := newTestLLM(fake)

	echoTool := &tool.Tool{
		Spec: tool.ToolSpec{Name: "echo"},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			text, _ := args["text"].(string)
			return "ECHO:" + text, nil
		},
	}

	result, err := llm.RunTools(context.Background(), "call echo", WithTools(echoTool))
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

func TestStreamEndToEnd(t *testing.T) {
	ch := make(chan provider.StreamChunk, 3)
	ch <- provider.StreamChunk{Text: "hello "}
	ch <- provider.StreamChunk{Text: "world"}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 7}}
	close(ch)

	fake := &fakeProvider{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	llm := newTestLLM(fake)

	textCh, state, err := llm.Stream(context.Background(), "say hello")
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
}

func TestStreamEventsEndToEnd(t *testing.T) {
	ch := make(chan provider.StreamChunk, 4)
	ch <- provider.StreamChunk{Text: "Checking "}
	ch <- provider.StreamChunk{ToolCalls: []provider.ToolCall{
		{ID: "call_1", Type: "function", Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"tokyo"}`}},
	}}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 12}}
	close(ch)

	fake := &fakeProvider{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	llm := newTestLLM(fake)

	echoTool := &tool.Tool{
		Spec: tool.ToolSpec{Name: "echo"},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			text, _ := args["text"].(string)
			return text + "!", nil
		},
	}

	eventCh, state, err := llm.StreamEvents(context.Background(), "call echo", WithTools(echoTool))
	if err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for e := range eventCh {
		kinds = append(kinds, string(e.Kind))
	}

	if !contains(kinds, "text") {
		t.Error("missing text event")
	}
	if !contains(kinds, "tool_call") {
		t.Error("missing tool_call event")
	}
	if !contains(kinds, "tool_result") {
		t.Error("missing tool_result event")
	}
	if !contains(kinds, "final") {
		t.Error("missing final event")
	}
	if state.Error != nil {
		t.Errorf("unexpected error: %v", state.Error)
	}
}

func TestIfEndToEnd(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{
		respToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "if_decision", Arguments: `{"value": true}`},
		}),
	}}
	llm := newTestLLM(fake)

	result, err := llm.If(context.Background(), "The service is down", "Should we page on-call?")
	if err != nil {
		t.Fatal(err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestClassifyEndToEnd(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{
		respToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "classify_decision", Arguments: `{"label": "support"}`},
		}),
	}}
	llm := newTestLLM(fake)

	label, err := llm.Classify(context.Background(), "Need invoice help", []string{"support", "sales"})
	if err != nil {
		t.Fatal(err)
	}
	if label != "support" {
		t.Errorf("label = %q", label)
	}
}

func TestTapeSessionChat(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("step one"), resp("step two")}}
	llm := newTestLLM(fake)

	session := llm.Tape("ops")
	session.Handoff("incident_42", nil)

	first, err := session.Chat(context.Background(), "Investigate DB timeout")
	if err != nil {
		t.Fatal(err)
	}
	if first != "step one" {
		t.Errorf("first = %q", first)
	}

	second, err := session.Chat(context.Background(), "Include rollback criteria")
	if err != nil {
		t.Fatal(err)
	}
	if second != "step two" {
		t.Errorf("second = %q", second)
	}
}

func TestTapeSessionHandoff(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("ok")}}
	llm := newTestLLM(fake)

	session := llm.Tape("ops")
	session.Handoff("incident_42", map[string]any{"owner": "tier1"})

	_, err := session.Chat(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := llm.tape.Store.FetchAll("ops", nil)
	hasAnchor := false
	for _, e := range entries {
		if e.Kind == "anchor" {
			hasAnchor = true
		}
	}
	if !hasAnchor {
		t.Error("expected anchor in tape")
	}
}

func TestTapeSessionQuery(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("answer")}}
	llm := newTestLLM(fake)

	session := llm.Tape("ops")
	session.Handoff("step1", nil)
	_, _ = session.Chat(context.Background(), "question")

	entries, err := session.Query.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected entries in tape query")
	}
}

func TestWithToolsOption(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{
		respToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "echo", Arguments: `{"text":"hi"}`},
		}),
	}}
	llm := newTestLLM(fake)

	echoTool := &tool.Tool{Spec: tool.ToolSpec{Name: "echo"}, Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
		return args["text"], nil
	}}

	calls, err := llm.ToolCalls(context.Background(), "test", WithTools(echoTool))
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Error("expected 1 tool call")
	}
	// Verify tool schema was sent
	if len(fake.calls[0].Tools) != 1 {
		t.Error("expected 1 tool schema in request")
	}
}

func TestWithMaxTokensOption(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("ok")}}
	llm := newTestLLM(fake)

	_, _ = llm.Chat(context.Background(), "hi", WithMaxTokens(42))
	if fake.calls[0].MaxTokens != 42 {
		t.Errorf("MaxTokens = %d", fake.calls[0].MaxTokens)
	}
}

func TestWithExtraHeadersOption(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("ok")}}
	llm := newTestLLM(fake)

	_, _ = llm.Chat(context.Background(), "hi", WithExtraHeaders(map[string]string{"X-Title": "Republic"}))
	if fake.calls[0].ExtraHeaders["X-Title"] != "Republic" {
		t.Error("extra headers not passed")
	}
}

func TestChatWithMessagesOverride(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("answer")}}
	llm := newTestLLM(fake)

	msgs := []map[string]any{
		{"role": "user", "content": "q1"},
		{"role": "assistant", "content": "a1"},
		{"role": "user", "content": "q2"},
	}

	result, err := llm.Chat(context.Background(), "", WithMessages(msgs))
	if err != nil {
		t.Fatal(err)
	}
	if result != "answer" {
		t.Errorf("result = %q", result)
	}
	if len(fake.calls[0].Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(fake.calls[0].Messages))
	}
}

func TestStreamStateUsageAndError(t *testing.T) {
	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{Text: "ok"}
	ch <- provider.StreamChunk{Usage: &provider.Usage{TotalTokens: 42}}
	close(ch)

	fake := &fakeProvider{streamQueue: []any{(<-chan provider.StreamChunk)(ch)}}
	llm := newTestLLM(fake)

	textCh, state, err := llm.Stream(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	for range textCh {
	}
	if state.Usage["total_tokens"] != 42 {
		t.Errorf("usage = %v", state.Usage)
	}
	if state.Error != nil {
		t.Error("unexpected error")
	}
}

func TestWithSystemPromptOption(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("ok")}}
	llm := newTestLLM(fake)

	_, _ = llm.Chat(context.Background(), "hi", WithSystemPrompt("be helpful"))
	if fake.calls[0].Messages[0].Role != "system" {
		t.Error("system prompt not at index 0")
	}
	if fake.calls[0].Messages[0].Content != "be helpful" {
		t.Error("system prompt content mismatch")
	}
}

func TestWithTemperatureOption(t *testing.T) {
	fake := &fakeProvider{completionQueue: []any{resp("ok")}}
	llm := newTestLLM(fake)

	_, _ = llm.Chat(context.Background(), "hi", WithTemperature(0.5))
	if fake.calls[0].Temperature == nil || *fake.calls[0].Temperature != 0.5 {
		t.Error("temperature not set")
	}
}

// helpers

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// Ensure imports are used
var _ = client.NewChatClient
