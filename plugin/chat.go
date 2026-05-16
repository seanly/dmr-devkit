package plugin

import (
	"context"

	"github.com/seanly/dmr-devkit/agent"
)

// ToolCallEvent is an alias to agent.ToolCallEvent so chat plugins can import
// only plugin and still use the same underlying type.
type ToolCallEvent = agent.ToolCallEvent

// ChatInterface defines the interface for chat interaction providers.
type ChatInterface interface {
	// Run starts an interactive chat session.
	// Returns when the session ends (user exits or context cancelled).
	Run(ctx context.Context, agent any, tape string) error

	// SendMessage sends a user message and returns the assistant's response.
	// Used for programmatic/API-driven interactions.
	SendMessage(ctx context.Context, message string) (string, error)

	// OnToolCall registers a callback for tool execution events.
	OnToolCall(callback func(event ToolCallEvent))

	// OnError registers a callback for error events.
	OnError(callback func(err error))
}
