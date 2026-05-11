package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSystemPrompt(t *testing.T) {
	prompt := DefaultSystemPrompt()
	if prompt == "" {
		t.Fatal("DefaultSystemPrompt() returned empty string")
	}
	// Should contain key content
	if !contains(prompt, "Core Principles") {
		t.Error("DefaultSystemPrompt() should contain Core Principles section")
	}
}

func TestDefaultSystemPrompt_Content(t *testing.T) {
	prompt := DefaultSystemPrompt()

	// Verify key sections exist
	sections := []string{
		"Core Principles",
		"Priority when guidance conflicts",
		"Evidence before acting",
		"Communication Style",
		"Plan Workflow",
		"Tool Usage",
	}
	for _, section := range sections {
		if !contains(prompt, section) {
			t.Errorf("DefaultSystemPrompt() missing section: %s", section)
		}
	}
}

func TestSystemPromptValue_Resolve(t *testing.T) {
	dir := t.TempDir()

	// Test Resolve with custom prompt string
	spv := SystemPromptValue{Raw: "custom prompt"}
	resolved, err := spv.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "custom prompt" {
		t.Errorf("got %q want %q", resolved, "custom prompt")
	}
}

func TestSystemPromptValue_ResolveWithFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a prompt file
	promptFile := filepath.Join(dir, "my_prompt.md")
	promptContent := "file-based prompt content"
	if err := os.WriteFile(promptFile, []byte(promptContent), 0o600); err != nil {
		t.Fatal(err)
	}

	spv := SystemPromptValue{Files: []string{"my_prompt.md"}}
	resolved, err := spv.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != promptContent {
		t.Errorf("got %q want %q", resolved, promptContent)
	}
}

func TestSystemPromptValue_ResolveMultipleFiles(t *testing.T) {
	dir := t.TempDir()

	// Create two prompt files
	promptFile1 := filepath.Join(dir, "part1.md")
	promptFile2 := filepath.Join(dir, "part2.md")
	if err := os.WriteFile(promptFile1, []byte("part1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptFile2, []byte("part2"), 0o600); err != nil {
		t.Fatal(err)
	}

	spv := SystemPromptValue{Files: []string{"part1.md", "part2.md"}}
	resolved, err := spv.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(resolved, "part1") || !contains(resolved, "part2") {
		t.Errorf("expected both parts in resolved prompt, got %q", resolved)
	}
}

func TestSystemPromptValue_ResolveEmpty(t *testing.T) {
	spv := SystemPromptValue{}
	resolved, err := spv.Resolve("/anywhere")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "" {
		t.Errorf("got %q want empty string", resolved)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
