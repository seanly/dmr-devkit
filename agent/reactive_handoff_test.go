package agent

import (
	"testing"
)

func TestIsContextOverflowError(t *testing.T) {
	// Test with nil error
	if isContextOverflowError(nil) {
		t.Error("nil error should not be context overflow")
	}

	// Note: Testing with actual errors would require creating mock errors
	// or using the actual core.Error type. For now, we test the basic case.
}

func TestBuildContinuationPrompt(t *testing.T) {
	a := New(nil, nil, nil, Config{})

	// Test with full info
	info := &contextOverflowInfo{
		userRequest:        "Please analyze the logs",
		assistantReasoning: "I'll search for error patterns",
		toolCalls:          []string{"grep(errors), cat(logs.txt)"},
		toolResults:        []string{"Found 5 errors", "Line 123: timeout"},
	}

	prompt := a.buildContinuationPrompt(info)

	// Check that all sections are included
	if !contains(prompt, "Context overflow occurred") {
		t.Error("Prompt should mention context overflow")
	}
	if !contains(prompt, "Please analyze the logs") {
		t.Error("Prompt should include user request")
	}
	if !contains(prompt, "I'll search for error patterns") {
		t.Error("Prompt should include assistant reasoning")
	}
	if !contains(prompt, "grep(errors), cat(logs.txt)") {
		t.Error("Prompt should include tool calls")
	}
	if !contains(prompt, "Found 5 errors") {
		t.Error("Prompt should include tool results")
	}

	// Test with empty info
	emptyInfo := &contextOverflowInfo{
		toolCalls:   make([]string, 0),
		toolResults: make([]string, 0),
	}

	emptyPrompt := a.buildContinuationPrompt(emptyInfo)
	if !contains(emptyPrompt, "Context overflow occurred") {
		t.Error("Prompt should still work with empty info")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
