package agent

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/config"
)

func TestToolResultMaxCharsForTape_DefaultFallback(t *testing.T) {
	a := New(nil, nil, nil, Config{
		AgentPolicy: config.AgentConfig{ToolResultMaxChars: 0},
		Models: []config.ModelConfig{
			{Name: "minimax", Model: "MiniMax-M2.5", Default: true, ToolResultMaxChars: 0},
		},
	})

	got := a.toolResultMaxCharsForTape("web")
	if got != defaultToolResultMaxChars {
		t.Fatalf("got %d, want %d", got, defaultToolResultMaxChars)
	}
}

func TestToolResultMaxCharsForTape_AgentOverride(t *testing.T) {
	a := New(nil, nil, nil, Config{
		AgentPolicy: config.AgentConfig{ToolResultMaxChars: 50000},
		Models: []config.ModelConfig{
			{Name: "minimax", Model: "MiniMax-M2.5", Default: true, ToolResultMaxChars: 0},
		},
	})

	got := a.toolResultMaxCharsForTape("web")
	if got != 50000 {
		t.Fatalf("got %d, want %d", got, 50000)
	}
}

func TestToolResultMaxCharsForTape_ModelOverride(t *testing.T) {
	a := New(nil, nil, nil, Config{
		AgentPolicy: config.AgentConfig{ToolResultMaxChars: 50000},
		Models: []config.ModelConfig{
			{Name: "minimax", Model: "MiniMax-M2.5", Default: true, ToolResultMaxChars: 60000},
		},
	})

	got := a.toolResultMaxCharsForTape("web")
	if got != 60000 {
		t.Fatalf("got %d, want %d", got, 60000)
	}
}

func TestToolResultMaxCharsForTape_ModelDisable(t *testing.T) {
	a := New(nil, nil, nil, Config{
		AgentPolicy: config.AgentConfig{ToolResultMaxChars: 50000},
		Models: []config.ModelConfig{
			{Name: "minimax", Model: "MiniMax-M2.5", Default: true, ToolResultMaxChars: -1},
		},
	})

	got := a.toolResultMaxCharsForTape("web")
	if got != -1 {
		t.Fatalf("got %d, want %d", got, -1)
	}
}

func TestToolResultMaxCharsForTape_TapeSpecificOverride(t *testing.T) {
	a := New(nil, nil, nil, Config{
		AgentPolicy: config.AgentConfig{ToolResultMaxChars: 50000},
		Models: []config.ModelConfig{
			{Name: "modelA", Model: "A", Default: true, ToolResultMaxChars: 40000},
			{Name: "modelB", Model: "B", Default: false, ToolResultMaxChars: 70000},
		},
	})
	// Force tape "web" to use modelB.
	a.mu.Lock()
	a.modelOverrides["web"] = "modelB"
	a.mu.Unlock()

	got := a.toolResultMaxCharsForTape("web")
	if got != 70000 {
		t.Fatalf("got %d, want %d", got, 70000)
	}
}

func TestTruncateForProvider_DisableAndTruncate(t *testing.T) {
	long := strings.Repeat("a", 50)

	// -1 disables truncation.
	out := truncateForProvider(long, -1)
	if out != long {
		t.Fatalf("disable: got truncated output unexpectedly")
	}

	out2 := truncateForProvider(long, 10)
	if out2 == long {
		t.Fatalf("expected truncation but output was unchanged")
	}
	if !strings.Contains(out2, "[truncated") {
		t.Fatalf("expected truncation marker, got %q", out2)
	}
	if !strings.Contains(out2, "Tool output was truncated") {
		t.Fatalf("expected truncation hint, got %q", out2)
	}
}
