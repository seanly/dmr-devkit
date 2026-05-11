package config

import _ "embed"

//go:embed system_prompt.v1.md
var defaultSystemPrompt string

// DefaultSystemPrompt returns the embedded default system prompt.
// This is used when no system_prompt is configured in config.toml.
func DefaultSystemPrompt() string {
	return defaultSystemPrompt
}
