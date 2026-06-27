package devkit

import (
	"context"
	"strings"
	"testing"
)

func TestQuickAgent_MissingModelErrors(t *testing.T) {
	_, err := QuickAgent(context.Background(), QuickAgentConfig{})
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !strings.Contains(err.Error(), "Model is required") {
		t.Fatalf("expected model required error, got %v", err)
	}
}

func TestQuickCrew_EmptyAgentsErrors(t *testing.T) {
	_, err := QuickCrew(context.Background(), QuickCrewConfig{}, "task")
	if err == nil {
		t.Fatal("expected error for empty agents")
	}
	if !strings.Contains(err.Error(), "requires at least one agent") {
		t.Fatalf("expected empty agents error, got %v", err)
	}
}

func TestQuickCrew_MissingModelErrors(t *testing.T) {
	cfg := QuickCrewConfig{
		Agents: []QuickAgentConfig{{Model: ""}},
	}
	_, err := QuickCrew(context.Background(), cfg, "task")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}
