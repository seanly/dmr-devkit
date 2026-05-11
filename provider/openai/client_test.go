package openai

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	goopenai "github.com/sashabaranov/go-openai"
)

func TestNewClientDefaultBaseURL(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test-key"})
	if c.BaseURL() != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
}

func TestNewClientCustomBaseURL(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test-key", BaseURL: "https://openrouter.ai/api/v1"})
	if c.BaseURL() != "https://openrouter.ai/api/v1" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
}

func TestMessageConversion(t *testing.T) {
	msg := Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "echo", Arguments: `{"text":"hi"}`}},
		},
	}
	if msg.ToolCalls[0].Function.Name != "echo" {
		t.Error("tool call name mismatch")
	}
}

func TestToolCallFunctionFields(t *testing.T) {
	fn := ToolCallFunction{Name: "get_weather", Arguments: `{"city":"Tokyo"}`}
	if fn.Name != "get_weather" {
		t.Error("name mismatch")
	}
	if fn.Arguments != `{"city":"Tokyo"}` {
		t.Error("arguments mismatch")
	}
}

func TestUsageFields(t *testing.T) {
	u := Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}
	if u.TotalTokens != 30 {
		t.Error("total tokens mismatch")
	}
}

func TestChatResponseFields(t *testing.T) {
	resp := &ChatResponse{
		Text:      "hello",
		Reasoning: "internal think",
		ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{Name: "echo"}},
		},
		Usage: &Usage{TotalTokens: 5},
	}
	if resp.Text != "hello" {
		t.Error("text mismatch")
	}
	if resp.Reasoning != "internal think" {
		t.Error("reasoning mismatch")
	}
	if len(resp.ToolCalls) != 1 {
		t.Error("tool calls count mismatch")
	}
	if resp.Usage.TotalTokens != 5 {
		t.Error("usage mismatch")
	}
}

func TestParseChatResponseReasoningContent(t *testing.T) {
	cr := parseChatResponse(goopenai.ChatCompletionResponse{
		Choices: []goopenai.ChatCompletionChoice{{
			Message: goopenai.ChatCompletionMessage{
				Role:             goopenai.ChatMessageRoleAssistant,
				Content:          "visible reply",
				ReasoningContent: "hidden chain of thought",
			},
		}},
		Usage: goopenai.Usage{
			PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3,
		},
	})
	if cr.Text != "visible reply" {
		t.Fatalf("Text = %q", cr.Text)
	}
	if cr.Reasoning != "hidden chain of thought" {
		t.Fatalf("Reasoning = %q", cr.Reasoning)
	}
	if cr.Usage == nil || cr.Usage.TotalTokens != 3 {
		t.Fatalf("usage: %#v", cr.Usage)
	}
}

func TestStreamChunkFields(t *testing.T) {
	chunk := StreamChunk{
		Text:      "hello ",
		Reasoning: "think delta",
		ToolCalls: []ToolCall{
			{ID: "call_1", Function: ToolCallFunction{Name: "echo", Arguments: `{"text":`}},
		},
	}
	if chunk.Text != "hello " {
		t.Error("text mismatch")
	}
	if chunk.Reasoning != "think delta" {
		t.Error("reasoning mismatch")
	}
	if len(chunk.ToolCalls) != 1 {
		t.Error("tool calls count mismatch")
	}
}

func TestStreamChunkWithUsage(t *testing.T) {
	chunk := StreamChunk{
		Usage: &Usage{TotalTokens: 42},
	}
	if chunk.Usage.TotalTokens != 42 {
		t.Error("usage mismatch")
	}
}

func TestStreamChunkWithError(t *testing.T) {
	chunk := StreamChunk{Err: &testError{"stream error"}}
	if chunk.Err == nil || chunk.Err.Error() != "stream error" {
		t.Error("error mismatch")
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestEmbedRequestFields(t *testing.T) {
	req := EmbedRequest{Model: "text-embedding-3-small", Input: []string{"hello", "world"}}
	if req.Model != "text-embedding-3-small" {
		t.Error("model mismatch")
	}
	if len(req.Input) != 2 {
		t.Error("input count mismatch")
	}
}

func TestEmbedResponseFields(t *testing.T) {
	resp := &EmbedResponse{
		Data: []EmbedData{
			{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
		},
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) != 3 {
		t.Error("embedding data mismatch")
	}
}

func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		name              string
		text              string
		existingReasoning string
		wantText          string
		wantReasoning     string
	}{
		{
			name:          "no think tags",
			text:          "hello world",
			wantText:      "hello world",
			wantReasoning: "",
		},
		{
			name:          "single think block",
			text:          "<think>internal reasoning</think>visible reply",
			wantText:      "visible reply",
			wantReasoning: "internal reasoning",
		},
		{
			name:          "think block in middle",
			text:          "before<think>hidden</think>after",
			wantText:      "beforeafter",
			wantReasoning: "hidden",
		},
		{
			name:          "multiple think blocks",
			text:          "<think>first</think>hello<think>second</think>world",
			wantText:      "helloworld",
			wantReasoning: "first\nsecond",
		},
		{
			name:              "appends to existing reasoning",
			text:              "<think>inline thought</think>reply",
			existingReasoning: "api reasoning",
			wantText:          "reply",
			wantReasoning:     "api reasoning\ninline thought",
		},
		{
			name:          "multiline think content",
			text:          "<think>\nstep 1\nstep 2\n</think>answer",
			wantText:      "answer",
			wantReasoning: "step 1\nstep 2",
		},
		{
			name:          "empty think block",
			text:          "<think></think>hello",
			wantText:      "hello",
			wantReasoning: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotReasoning := stripThinkTags(tt.text, tt.existingReasoning)
			if gotText != tt.wantText {
				t.Errorf("text = %q, want %q", gotText, tt.wantText)
			}
			if gotReasoning != tt.wantReasoning {
				t.Errorf("reasoning = %q, want %q", gotReasoning, tt.wantReasoning)
			}
		})
	}
}

func TestParseChatResponseStripsThinkTags(t *testing.T) {
	cr := parseChatResponse(goopenai.ChatCompletionResponse{
		Choices: []goopenai.ChatCompletionChoice{{
			Message: goopenai.ChatCompletionMessage{
				Role:    goopenai.ChatMessageRoleAssistant,
				Content: "<think>internal reasoning</think>visible reply",
			},
		}},
		Usage: goopenai.Usage{TotalTokens: 5},
	})
	if cr.Text != "visible reply" {
		t.Fatalf("Text = %q, want %q", cr.Text, "visible reply")
	}
	if cr.Reasoning != "internal reasoning" {
		t.Fatalf("Reasoning = %q, want %q", cr.Reasoning, "internal reasoning")
	}
}

func TestBuildRequestMessages(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test"})
	req := ChatRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi", ToolCalls: []ToolCall{
				{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "echo", Arguments: "{}"}},
			}},
			{Role: "tool", Content: `{"result":"ok"}`, ToolCallID: "c1"},
		},
	}
	goReq := c.buildRequest(req)
	if len(goReq.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(goReq.Messages))
	}
	if goReq.Messages[0].Role != "user" {
		t.Error("first message role mismatch")
	}
	if len(goReq.Messages[1].ToolCalls) != 1 {
		t.Error("tool calls not converted")
	}
	if goReq.Messages[2].ToolCallID != "c1" {
		t.Error("tool_call_id not set")
	}
}

func TestBuildRequestTools(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test"})
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "echo",
					"description": "Echo text",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	}
	goReq := c.buildRequest(req)
	if len(goReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(goReq.Tools))
	}
	if goReq.Tools[0].Function.Name != "echo" {
		t.Error("tool name mismatch")
	}
}

func TestBuildRequestMaxTokens(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test"})
	req := ChatRequest{
		Model:     "gpt-4o",
		Messages:  []Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	}
	goReq := c.buildRequest(req)
	if goReq.MaxCompletionTokens != 100 {
		t.Errorf("MaxCompletionTokens = %d", goReq.MaxCompletionTokens)
	}
}

func TestBuildRequestTemperature(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test"})
	temp := float32(0.7)
	req := ChatRequest{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: &temp,
	}
	goReq := c.buildRequest(req)
	if goReq.Temperature != 0.7 {
		t.Errorf("Temperature = %f", goReq.Temperature)
	}
}

func TestBuildRequestReasoningContent(t *testing.T) {
	c := NewClient(ClientConfig{APIKey: "test"})
	req := ChatRequest{
		Model: "deepseek-reasoner",
		Messages: []Message{{
			Role:             "assistant",
			Content:          "call the tool",
			ReasoningContent: "step-by-step plan",
		}},
	}
	goReq := c.buildRequest(req)
	if len(goReq.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(goReq.Messages))
	}
	if goReq.Messages[0].ReasoningContent != "step-by-step plan" {
		t.Errorf("ReasoningContent = %q", goReq.Messages[0].ReasoningContent)
	}
	raw, err := json.Marshal(goReq.Messages[0])
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if string(out["reasoning_content"]) != `"step-by-step plan"` {
		t.Errorf("wire json reasoning_content: %s", out["reasoning_content"])
	}
}

func TestExtractFallbackToolCalls_QwenNoCloseTags(t *testing.T) {
	text := `  <tool_call> <function=shell> <parameter=cmd> ls -la  <parameter=reason> List directory to show the created HelloWorld.java file
  </tool_call>`
	calls, cleaned := extractFallbackToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "shell" {
		t.Errorf("name = %q, want shell", calls[0].Function.Name)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments unmarshal: %v", err)
	}
	if args["cmd"] != "ls -la" {
		t.Errorf("cmd = %q, want 'ls -la'", args["cmd"])
	}
	if args["reason"] != "List directory to show the created HelloWorld.java file" {
		t.Errorf("reason = %q", args["reason"])
	}
	if cleaned != "" {
		t.Errorf("cleaned = %q, want empty", cleaned)
	}
}

func TestExtractFallbackToolCalls_QwenWithCloseTags(t *testing.T) {
	text := `<tool_call><function=read><parameter=path>/tmp/test.txt</parameter></function></tool_call>`
	calls, cleaned := extractFallbackToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read" {
		t.Errorf("name = %q, want read", calls[0].Function.Name)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments unmarshal: %v", err)
	}
	if args["path"] != "/tmp/test.txt" {
		t.Errorf("path = %q", args["path"])
	}
	if cleaned != "" {
		t.Errorf("cleaned = %q, want empty", cleaned)
	}
}

func TestExtractFallbackToolCalls_JSONInsideTag(t *testing.T) {
	text := `<tool_call>{"name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"}</tool_call>`
	calls, cleaned := extractFallbackToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "get_weather" {
		t.Errorf("name = %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"city":"Tokyo"}` {
		t.Errorf("arguments = %q", calls[0].Function.Arguments)
	}
	if cleaned != "" {
		t.Errorf("cleaned = %q, want empty", cleaned)
	}
}

func TestExtractFallbackToolCalls_MixedText(t *testing.T) {
	text := `I'll check that for you.
<tool_call><function=shell><parameter=cmd>pwd</parameter></tool_call>
Done!`
	calls, cleaned := extractFallbackToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "shell" {
		t.Errorf("name = %q", calls[0].Function.Name)
	}
	wantCleaned := "I'll check that for you.\n\nDone!"
	if cleaned != wantCleaned {
		t.Errorf("cleaned = %q, want %q", cleaned, wantCleaned)
	}
}

func TestExtractFallbackToolCalls_NoMatch(t *testing.T) {
	text := "Just a plain response with <tool_call> malformed"
	calls, cleaned := extractFallbackToolCalls(text)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
	if cleaned != text {
		t.Errorf("cleaned should be unchanged")
	}
}

func TestParseChatResponse_FallbackToolCalls(t *testing.T) {
	cr := parseChatResponse(goopenai.ChatCompletionResponse{
		Choices: []goopenai.ChatCompletionChoice{{
			Message: goopenai.ChatCompletionMessage{
				Role:    goopenai.ChatMessageRoleAssistant,
				Content: `<tool_call><function=shell><parameter=cmd>ls</parameter></tool_call>`,
			},
		}},
		Usage: goopenai.Usage{TotalTokens: 5},
	})
	if len(cr.ToolCalls) != 1 {
		t.Fatalf("expected 1 fallback tool call, got %d", len(cr.ToolCalls))
	}
	if cr.ToolCalls[0].Function.Name != "shell" {
		t.Errorf("name = %q", cr.ToolCalls[0].Function.Name)
	}
	if cr.Text != "" {
		t.Errorf("Text = %q, want empty", cr.Text)
	}
}

func TestNewClientSetsTimeout(t *testing.T) {
	// BearerToken path
	c1 := NewClient(ClientConfig{
		BearerToken: func() (string, error) { return "tok", nil },
	})
	if c1.config.BearerToken == nil {
		t.Fatal("expected BearerToken")
	}

	// APIKey path
	c2 := NewClient(ClientConfig{APIKey: "k"})
	if c2.config.APIKey != "k" {
		t.Fatal("expected APIKey")
	}
}

func TestHTTPClientFromConfigDefaults(t *testing.T) {
	c := httpClientFromConfig(ClientConfig{})
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T", c.Transport)
	}
	if tr.ResponseHeaderTimeout != DefaultHTTPResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, DefaultHTTPResponseHeaderTimeout)
	}
	if c.Timeout != DefaultHTTPClientTimeout {
		t.Errorf("Client.Timeout = %v, want %v", c.Timeout, DefaultHTTPClientTimeout)
	}
}

func TestHTTPClientFromConfigOverride(t *testing.T) {
	c := httpClientFromConfig(ClientConfig{
		HTTPResponseHeaderTimeout: 2 * time.Minute,
		HTTPClientTimeout:         5 * time.Minute,
	})
	tr := c.Transport.(*http.Transport)
	if tr.ResponseHeaderTimeout != 2*time.Minute {
		t.Errorf("ResponseHeaderTimeout = %v", tr.ResponseHeaderTimeout)
	}
	if c.Timeout != 5*time.Minute {
		t.Errorf("Client.Timeout = %v", c.Timeout)
	}
}

func TestHTTPClientFromConfigRaisesTotalWhenBelowHeaderWait(t *testing.T) {
	c := httpClientFromConfig(ClientConfig{
		HTTPResponseHeaderTimeout: 20 * time.Minute,
		HTTPClientTimeout:         0,
	})
	tr := c.Transport.(*http.Transport)
	if tr.ResponseHeaderTimeout != 20*time.Minute {
		t.Errorf("ResponseHeaderTimeout = %v", tr.ResponseHeaderTimeout)
	}
	if c.Timeout < tr.ResponseHeaderTimeout {
		t.Errorf("Client.Timeout %v < header wait %v", c.Timeout, tr.ResponseHeaderTimeout)
	}
}

func TestNewClientPreservesHTTPTimeoutsInConfig(t *testing.T) {
	c := NewClient(ClientConfig{
		APIKey:                    "k",
		HTTPResponseHeaderTimeout: 3 * time.Minute,
		HTTPClientTimeout:         4 * time.Minute,
	})
	if c.config.HTTPResponseHeaderTimeout != 3*time.Minute {
		t.Errorf("HTTPResponseHeaderTimeout = %v", c.config.HTTPResponseHeaderTimeout)
	}
	if c.config.HTTPClientTimeout != 4*time.Minute {
		t.Errorf("HTTPClientTimeout = %v", c.config.HTTPClientTimeout)
	}
}
