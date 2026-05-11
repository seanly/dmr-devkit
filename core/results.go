package core

import "fmt"

// ErrorPayload represents an error with structured details, used as a value
// (not necessarily raised as an exception). Mirrors Python ErrorPayload.
type ErrorPayload struct {
	Kind    ErrorKind
	Message string
	Details map[string]any
}

func (e *ErrorPayload) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// ToolCallFunction holds the function name and JSON-encoded arguments.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCallData represents a single tool call from the LLM.
type ToolCallData struct {
	ID       string           `json:"id"`
	Function ToolCallFunction `json:"function"`
}

// StreamEventKind is the type tag for stream events.
type StreamEventKind string

const (
	StreamText       StreamEventKind = "text"
	StreamToolCall   StreamEventKind = "tool_call"
	StreamToolResult StreamEventKind = "tool_result"
	StreamUsage      StreamEventKind = "usage"
	StreamError      StreamEventKind = "error"
	StreamFinal      StreamEventKind = "final"
)

// StreamEvent represents a single event in a streaming response.
type StreamEvent struct {
	Kind StreamEventKind
	Data map[string]any
}

// ToolAutoResult is the result of an automatic tool-execution round.
type ToolAutoResult struct {
	Kind        string // "text", "tools", "error"
	Text        string
	Reasoning   string // from API reasoning_content; for tape/audit only, not user-visible Text
	ToolCalls   []ToolCallData
	ToolResults []any
	Error       *ErrorPayload
	Usage       map[string]any // token usage from LLM response
}

// ToolExecution holds the outcome of executing one set of tool calls.
type ToolExecution struct {
	ToolCalls   []ToolCallData
	ToolResults []any
	Error       *ErrorPayload
}
