package agent

import (
	"testing"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tool"
)

func TestFilterAllowedTools(t *testing.T) {
	tools := []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "shell"}},
		{Spec: tool.ToolSpec{Name: "fsRead"}},
		{Spec: tool.ToolSpec{Name: "memoryRead"}},
	}

	// Empty allowed list → no filtering
	out := filterAllowedTools(tools, nil)
	if len(out) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(out))
	}

	// Allowed list with specific tools
	out = filterAllowedTools(tools, &runMode{allowedToolNames: map[string]struct{}{"memoryRead": {}, "shell": {}}})
	if len(out) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(out))
	}
	for _, o := range out {
		if o.Spec.Name != "memoryRead" && o.Spec.Name != "shell" {
			t.Errorf("unexpected tool %q allowed through", o.Spec.Name)
		}
	}

	// Allowed list with no matches
	out = filterAllowedTools(tools, &runMode{allowedToolNames: map[string]struct{}{"unknown": {}}})
	if len(out) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(out))
	}
}

func TestHasShellFailure(t *testing.T) {
	failureResult := "❌ COMMAND FAILED (exit code: 1)\noutput"

	// Shell tool failure → true
	calls := []core.ToolCallData{{Function: core.ToolCallFunction{Name: "shell"}}}
	results := []any{failureResult}
	if !hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=true for shell tool failure")
	}

	// Powershell tool failure → true
	calls = []core.ToolCallData{{Function: core.ToolCallFunction{Name: "powershellOutput"}}}
	results = []any{failureResult}
	if !hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=true for powershell tool failure")
	}

	// fsRead tool containing same string → false (must not false-positive)
	calls = []core.ToolCallData{{Function: core.ToolCallFunction{Name: "fsRead"}}}
	results = []any{failureResult}
	if hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=false for fsRead tool")
	}

	// No calls available but content matches → conservatively true (unknown tool)
	calls = nil
	results = []any{failureResult}
	if !hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=true when content matches even without calls")
	}

	// Non-failure content → false
	calls = []core.ToolCallData{{Function: core.ToolCallFunction{Name: "shell"}}}
	results = []any{"some normal output"}
	if hasShellFailure(calls, results) {
		t.Error("expected hasShellFailure=false for normal output")
	}
}
