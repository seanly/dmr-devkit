// Package provider defines provider-neutral chat completion types and the ChatProvider
// interface used by pkg/core. Implementations (e.g. pkg/openai) satisfy ChatProvider.
package provider

import "context"

// ChatProvider is the interface execution engines use to call an LLM backend.
// It can be implemented by *openai.Client or a test fake.
type ChatProvider interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}

// ContentPart is a part of a multi-modal message content.
type ContentPart interface {
	isContentPart()
}

// TextPart is a text content part.
type TextPart struct {
	Text string `json:"text"`
}

// ImagePart is an image content part.
// URL may be a data URI (data:image/xxx;base64,...) or an HTTP URL.
type ImagePart struct {
	URL string `json:"image_url"`
}

func (TextPart) isContentPart() {}
func (ImagePart) isContentPart() {}

// Message represents a chat message.
type Message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	Parts            []ContentPart `json:"-"` // multi-modal parts; when non-empty, takes precedence over Content
	ReasoningContent string        `json:"reasoning_content,omitempty"` // prior assistant turn; required by e.g. DeepSeek thinking mode
	ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call in a message.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction holds function name and arguments.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest is the request for a chat completion.
type ChatRequest struct {
	Model         string
	Messages      []Message
	Tools         []map[string]any
	ToolChoice    any
	MaxTokens     int
	Temperature   *float32
	TopP          *float32
	Stream        bool
	StreamOptions *StreamOptions
	ExtraHeaders  map[string]string
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	IncludeUsage bool
}

// Usage holds token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse is the response from a chat completion.
type ChatResponse struct {
	Text      string
	Reasoning string // model chain-of-thought from reasoning_content; not mixed into Text
	ToolCalls []ToolCall
	Usage     *Usage
}

// StreamChunk is a single chunk in a streaming response.
type StreamChunk struct {
	Text      string
	Reasoning string // delta of reasoning_content when streaming
	ToolCalls []ToolCall
	Usage     *Usage
	Err       error
}
