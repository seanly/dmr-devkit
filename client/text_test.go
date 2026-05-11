package client

import (
	"context"
	"testing"

	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tool"
)

func TestIfReturnsTrue(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "if_decision", Arguments: `{"value": true}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	result, err := tc.If(context.Background(), "The service is down", "Should we page on-call?")
	if err != nil {
		t.Fatal(err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestIfReturnsFalse(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "if_decision", Arguments: `{"value": false}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	result, err := tc.If(context.Background(), "CPU at 10%", "Should we scale down?")
	if err != nil {
		t.Fatal(err)
	}
	if result {
		t.Error("expected false")
	}
}

func TestIfBuildsCorrectPrompt(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "if_decision", Arguments: `{"value": true}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	_, _ = tc.If(context.Background(), "test input", "test question?")

	if len(fake.calls) != 1 {
		t.Fatal("expected 1 call")
	}
	userMsg := fake.calls[0].Messages[0]
	if userMsg.Role != "user" {
		t.Error("expected user role")
	}
	content := userMsg.Content
	if !(containsStr(content, "test input") && containsStr(content, "test question?")) {
		t.Errorf("prompt = %q", content)
	}
}

func TestIfBuildsCorrectSchema(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "if_decision", Arguments: `{"value": true}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	_, _ = tc.If(context.Background(), "x", "y?")

	if len(fake.calls[0].Tools) != 1 {
		t.Fatal("expected 1 tool")
	}
	toolSchema := fake.calls[0].Tools[0]
	fn, _ := toolSchema["function"].(map[string]any)
	if fn["name"] != "if_decision" {
		t.Errorf("tool name = %v", fn["name"])
	}
}

func TestClassifyReturnsMatchingLabel(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "classify_decision", Arguments: `{"label": "support"}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	label, err := tc.Classify(context.Background(), "Need invoice help", []string{"support", "sales"})
	if err != nil {
		t.Fatal(err)
	}
	if label != "support" {
		t.Errorf("label = %q", label)
	}
}

func TestClassifyRejectsInvalidLabel(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "classify_decision", Arguments: `{"label": "other"}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	_, err := tc.Classify(context.Background(), "Unknown intent", []string{"support", "sales"})
	if err == nil {
		t.Fatal("expected error for invalid label")
	}
}

func TestClassifyBuildsCorrectSchema(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "classify_decision", Arguments: `{"label": "support"}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	_, _ = tc.Classify(context.Background(), "x", []string{"support", "sales", "billing"})

	if len(fake.calls) != 1 {
		t.Fatal("expected 1 call")
	}
	if len(fake.calls[0].Tools) != 1 {
		t.Fatal("expected 1 tool")
	}
	fn := fake.calls[0].Tools[0]["function"].(map[string]any)
	if fn["name"] != "classify_decision" {
		t.Errorf("tool name = %v", fn["name"])
	}
}

func TestClassifyBuildsCorrectPrompt(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "classify_decision", Arguments: `{"label": "support"}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	_, _ = tc.Classify(context.Background(), "test input", []string{"support", "sales"})

	content := fake.calls[0].Messages[0].Content
	if !(containsStr(content, "test input") && containsStr(content, "support") && containsStr(content, "sales")) {
		t.Errorf("prompt = %q", content)
	}
}

func TestIfWithSystemPrompt(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "if_decision", Arguments: `{"value": true}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	_, _ = tc.If(context.Background(), "x", "y?", TextOpts{SystemPrompt: "be brief"})

	if fake.calls[0].Messages[0].Role != "system" {
		t.Error("expected system prompt at index 0")
	}
}

func TestClassifyWithMaxTokens(t *testing.T) {
	fake := &fakeClient{completionQueue: []any{
		respWithToolCalls(provider.ToolCall{
			ID: "call_1", Type: "function",
			Function: provider.ToolCallFunction{Name: "classify_decision", Arguments: `{"label": "support"}`},
		}),
	}}
	tc := NewTextClient(newTestChatClient(fake))

	_, _ = tc.Classify(context.Background(), "x", []string{"support"}, TextOpts{MaxTokens: 50})

	if fake.calls[0].MaxTokens != 50 {
		t.Errorf("MaxTokens = %d", fake.calls[0].MaxTokens)
	}
}

func containsStr(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && (s == sub || len(s) > len(sub) && (func() bool {
		for i := range len(s) - len(sub) + 1 {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}

// Ensure tool package is used
var _ = tool.NewToolExecutor
