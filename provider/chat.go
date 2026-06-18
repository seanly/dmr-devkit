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

// ContentPartFromMap converts a JSON-like map into a ContentPart.
// Expected formats:
//
//	{"type": "text", "text": "hello"}
//	{"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}
func ContentPartFromMap(m map[string]any) ContentPart {
	typ, _ := m["type"].(string)
	switch typ {
	case "text":
		if text, ok := m["text"].(string); ok {
			return TextPart{Text: text}
		}
	case "image_url":
		if iu, ok := m["image_url"].(map[string]any); ok {
			if url, ok := iu["url"].(string); ok {
				return ImagePart{URL: url}
			}
		}
	}
	return nil
}

// ContentPartToMap converts a ContentPart to a map suitable for JSON serialization.
func ContentPartToMap(p ContentPart) map[string]any {
	switch part := p.(type) {
	case TextPart:
		return map[string]any{"type": "text", "text": part.Text}
	case ImagePart:
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": part.URL}}
	}
	return nil
}

// imageOmittedPlaceholder is injected when a message had only image parts and the
// current model does not support vision (wire payload only; tape is unchanged).
const imageOmittedPlaceholder = "[Image omitted: current model does not support vision]"

// StripImagePartsFromMessages returns a copy of messages with image_url parts removed.
// Used when sending tape history to text-only models after a model switch.
func StripImagePartsFromMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return messages
	}
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		rawParts, ok := m["parts"].([]any)
		if !ok || len(rawParts) == 0 {
			out = append(out, m)
			continue
		}
		filtered := make([]any, 0, len(rawParts))
		hadImage := false
		for _, rp := range rawParts {
			pm, ok := rp.(map[string]any)
			if !ok {
				filtered = append(filtered, rp)
				continue
			}
			if typ, _ := pm["type"].(string); typ == "image_url" {
				hadImage = true
				continue
			}
			filtered = append(filtered, rp)
		}
		if !hadImage {
			out = append(out, m)
			continue
		}
		nm := make(map[string]any, len(m))
		for k, v := range m {
			nm[k] = v
		}
		if len(filtered) > 0 {
			nm["parts"] = filtered
		} else {
			delete(nm, "parts")
			content, _ := nm["content"].(string)
			if content == "" {
				nm["content"] = imageOmittedPlaceholder
			}
		}
		out = append(out, nm)
	}
	return out
}

// StripImageContentParts removes image parts from a ContentPart slice.
func StripImageContentParts(parts []ContentPart) []ContentPart {
	if len(parts) == 0 {
		return parts
	}
	out := make([]ContentPart, 0, len(parts))
	for _, p := range parts {
		if _, ok := p.(ImagePart); ok {
			continue
		}
		out = append(out, p)
	}
	return out
}

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
