package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	goopenai "github.com/sashabaranov/go-openai"
	"github.com/seanly/dmr-devkit/provider"
)

// Re-export protocol types for callers that import pkg/openai only.
type (
	Message          = provider.Message
	ToolCall         = provider.ToolCall
	ToolCallFunction = provider.ToolCallFunction
	ChatRequest      = provider.ChatRequest
	StreamOptions    = provider.StreamOptions
	Usage            = provider.Usage
	ChatResponse     = provider.ChatResponse
	StreamChunk      = provider.StreamChunk
	EmbedRequest     = provider.EmbedRequest
	EmbedData        = provider.EmbedData
	EmbedResponse    = provider.EmbedResponse
)

// thinkTagRe matches <think>...</think> blocks (including multiline) in model output.
var thinkTagRe = regexp.MustCompile(`(?s)<think>(.*?)</think>`)

// stripThinkTags extracts inline <think> blocks from text, returning the
// cleaned text and the concatenated thinking content. If the API already
// provided reasoning_content, the extracted thinking is appended to it.
func stripThinkTags(text, existingReasoning string) (cleaned string, reasoning string) {
	matches := thinkTagRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text, existingReasoning
	}
	var parts []string
	for _, m := range matches {
		parts = append(parts, strings.TrimSpace(m[1]))
	}
	extracted := strings.Join(parts, "\n")
	cleaned = strings.TrimSpace(thinkTagRe.ReplaceAllString(text, ""))
	if existingReasoning != "" {
		reasoning = existingReasoning + "\n" + extracted
	} else {
		reasoning = extracted
	}
	return cleaned, reasoning
}

// fallbackToolCall extraction helpers for models that emit tool calls as raw text.
var (
	toolCallBlockRe      = regexp.MustCompile(`(?s)<tool_call\b[^>]*>(.*?)</tool_call>`)
	functionNameRe1      = regexp.MustCompile(`<function=([^\s>]+)>`)
	functionNameRe2      = regexp.MustCompile(`<function>([^<]*)</function>`)
	functionNameRe3      = regexp.MustCompile(`<function>.*?<name>([^<]*)</name>.*?</function>`)
	parameterReNoClose   = regexp.MustCompile(`<parameter=([^\s>]+)>\s*([^<]*)`)
	parameterReClose     = regexp.MustCompile(`<parameter(?:=([^\s>]+))?>([^<]*)</parameter>`)
	parameterReNameValue = regexp.MustCompile(`<parameter>.*?<name>([^<]*)</name>.*?<value>([^<]*)</value>.*?</parameter>`)
)

// extractFallbackToolCalls attempts to parse tool calls embedded in the model's
// text content. Some models (e.g. Qwen) emit tool calls as XML-like tags in the
// message body instead of using the API's structured tool_calls field.
// It returns any extracted tool calls and the cleaned text with the tags removed.
func extractFallbackToolCalls(text string) ([]ToolCall, string) {
	if !strings.Contains(text, "<tool_call") {
		return nil, text
	}

	blocks := toolCallBlockRe.FindAllStringSubmatchIndex(text, -1)
	if len(blocks) == 0 {
		return nil, text
	}

	var calls []ToolCall
	var cleaned strings.Builder
	lastEnd := 0
	callIdx := 1

	for _, block := range blocks {
		start, end := block[0], block[1]
		cleaned.WriteString(text[lastEnd:start])
		content := text[block[2]:block[3]]

		call := parseToolCallBlock(content, callIdx)
		if call != nil {
			calls = append(calls, *call)
			callIdx++
		} else {
			// Couldn't parse this block; preserve it in the text.
			cleaned.WriteString(text[start:end])
		}
		lastEnd = end
	}
	cleaned.WriteString(text[lastEnd:])

	if len(calls) == 0 {
		return nil, text
	}

	cleanedText := strings.TrimSpace(cleaned.String())
	return calls, cleanedText
}

func parseToolCallBlock(content string, idx int) *ToolCall {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	// Try JSON format first: {"name": "...", "arguments": "..."}
	if strings.HasPrefix(content, "{") {
		var data struct {
			Name      string         `json:"name"`
			Function  string         `json:"function"`
			Arguments string         `json:"arguments"`
			Args      map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(content), &data); err == nil {
			name := data.Name
			if name == "" {
				name = data.Function
			}
			if name != "" {
				args := data.Arguments
				if args == "" && len(data.Args) > 0 {
					b, _ := json.Marshal(data.Args)
					args = string(b)
				}
				if args == "" {
					args = "{}"
				}
				return &ToolCall{
					ID:   fmt.Sprintf("call_fallback_%d", idx),
					Type: "function",
					Function: ToolCallFunction{
						Name:      name,
						Arguments: args,
					},
				}
			}
		}
	}

	// Try XML-like format.
	name := extractFunctionName(content)
	if name == "" {
		return nil
	}

	args := extractParameters(content)
	argsJSON, _ := json.Marshal(args)

	return &ToolCall{
		ID:   fmt.Sprintf("call_fallback_%d", idx),
		Type: "function",
		Function: ToolCallFunction{
			Name:      name,
			Arguments: string(argsJSON),
		},
	}
}

func extractFunctionName(content string) string {
	if m := functionNameRe1.FindStringSubmatch(content); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	if m := functionNameRe2.FindStringSubmatch(content); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	if m := functionNameRe3.FindStringSubmatch(content); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func extractParameters(content string) map[string]string {
	args := make(map[string]string)
	for _, m := range parameterReNoClose.FindAllStringSubmatch(content, -1) {
		args[strings.TrimSpace(m[1])] = strings.TrimSpace(m[2])
	}
	for _, m := range parameterReClose.FindAllStringSubmatch(content, -1) {
		key := strings.TrimSpace(m[1])
		if key != "" {
			args[key] = strings.TrimSpace(m[2])
		}
	}
	for _, m := range parameterReNameValue.FindAllStringSubmatch(content, -1) {
		args[strings.TrimSpace(m[1])] = strings.TrimSpace(m[2])
	}
	return args
}

// ClientConfig configures the OpenAI-compatible client.
type ClientConfig struct {
	APIKey  string
	BaseURL string // defaults to OpenAI; set for OpenRouter/Ollama etc.
	// BearerToken, if non-nil, is called before each HTTP request to obtain the Authorization Bearer value
	// (e.g. OAuth2 clientCredentials). When set, APIKey is not sent as Bearer by the transport (this doer replaces it).
	BearerToken func() (string, error)
	// Headers are additional HTTP headers sent with every request (e.g., User-Agent, X-Client-Name).
	Headers map[string]string
	// HTTPResponseHeaderTimeout is the time to wait for response headers after the request is sent.
	// Zero uses DefaultHTTPResponseHeaderTimeout (suitable for slow LLM backends).
	HTTPResponseHeaderTimeout time.Duration
	// HTTPClientTimeout caps the entire request (headers + reading the body). Zero uses DefaultHTTPClientTimeout.
	// If set shorter than HTTPResponseHeaderTimeout, it is raised to match.
	HTTPClientTimeout time.Duration
}

type bearerHTTPDoer struct {
	inner   *http.Client
	token   func() (string, error)
	headers map[string]string
}

func (b *bearerHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	tok, err := b.token()
	if err != nil {
		return nil, fmt.Errorf("bearer token: %w", err)
	}
	r2 := req.Clone(req.Context())
	r2.Header.Set("Authorization", "Bearer "+tok)
	for k, v := range b.headers {
		r2.Header.Set(k, v)
	}
	return b.inner.Do(r2)
}

// headerInjectTransport wraps an http.RoundTripper to inject custom headers.
type headerInjectTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerInjectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(t.headers) == 0 {
		return t.base.RoundTrip(req)
	}
	r2 := req.Clone(req.Context())
	for k, v := range t.headers {
		r2.Header.Set(k, v)
	}
	return t.base.RoundTrip(r2)
}

// Defaults for LLM APIs: large prompts can take minutes before the first response byte.
const (
	DefaultHTTPResponseHeaderTimeout = 10 * time.Minute
	DefaultHTTPClientTimeout         = 15 * time.Minute
)

// httpClientFromConfig builds an http.Client with optional per-provider timeouts.
// Zero durations mean defaults; total request time is coerced to be at least the
// response-header wait so the client does not abort before headers arrive.
func httpClientFromConfig(cfg ClientConfig) *http.Client {
	headerWait := cfg.HTTPResponseHeaderTimeout
	if headerWait == 0 {
		headerWait = DefaultHTTPResponseHeaderTimeout
	}
	total := cfg.HTTPClientTimeout
	if total == 0 {
		total = DefaultHTTPClientTimeout
	}
	if total < headerWait {
		total = headerWait
	}
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: headerWait,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   total,
		Transport: transport,
	}
}

// Client wraps go-openai for Republic.
type Client struct {
	inner  *goopenai.Client
	config ClientConfig
}

// NewClient creates a new OpenAI-compatible client.
func NewClient(cfg ClientConfig) *Client {
	var goConfig goopenai.ClientConfig
	if cfg.BearerToken != nil {
		goConfig = goopenai.DefaultConfig("")
		if cfg.BaseURL != "" {
			goConfig.BaseURL = cfg.BaseURL
		}
		goConfig.HTTPClient = &bearerHTTPDoer{
			inner:   httpClientFromConfig(cfg),
			token:   cfg.BearerToken,
			headers: cfg.Headers,
		}
	} else {
		goConfig = goopenai.DefaultConfig(cfg.APIKey)
		if cfg.BaseURL != "" {
			goConfig.BaseURL = cfg.BaseURL
		}
		// Inject custom headers (e.g., User-Agent) via transport wrapper.
		if len(cfg.Headers) > 0 {
			base := httpClientFromConfig(cfg)
			goConfig.HTTPClient = &http.Client{
				Timeout: base.Timeout,
				Transport: &headerInjectTransport{
					base:    base.Transport,
					headers: cfg.Headers,
				},
			}
		} else {
			goConfig.HTTPClient = httpClientFromConfig(cfg)
		}
	}
	return &Client{
		inner:  goopenai.NewClientWithConfig(goConfig),
		config: cfg,
	}
}

// ChatCompletion performs a non-streaming chat completion.
func (c *Client) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	goReq := c.buildRequest(req)
	resp, err := c.inner.CreateChatCompletion(ctx, goReq)
	if err != nil {
		return nil, err
	}
	return parseChatResponse(resp), nil
}

// ChatCompletionStream performs a streaming chat completion, returning a channel.
func (c *Client) ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	goReq := c.buildRequest(req)
	goReq.Stream = true
	if req.StreamOptions != nil && req.StreamOptions.IncludeUsage {
		goReq.StreamOptions = &goopenai.StreamOptions{IncludeUsage: true}
	}

	stream, err := c.inner.CreateChatCompletionStream(ctx, goReq)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				select {
				case ch <- StreamChunk{Err: err}:
				case <-ctx.Done():
				}
				return
			}
			chunk := parseStreamChunk(resp)
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// Embedding performs an embedding request.
func (c *Client) Embedding(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	goReq := goopenai.EmbeddingRequest{
		Model: goopenai.EmbeddingModel(req.Model),
		Input: req.Input,
	}
	resp, err := c.inner.CreateEmbeddings(ctx, goReq)
	if err != nil {
		return nil, err
	}
	data := make([]EmbedData, len(resp.Data))
	for i, d := range resp.Data {
		data[i] = EmbedData{
			Embedding: d.Embedding,
			Index:     d.Index,
		}
	}
	return &EmbedResponse{Data: data}, nil
}

func (c *Client) buildRequest(req ChatRequest) goopenai.ChatCompletionRequest {
	messages := make([]goopenai.ChatCompletionMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := goopenai.ChatCompletionMessage{
			Role:             m.Role,
			ReasoningContent: m.ReasoningContent,
		}
		if len(m.Parts) > 0 {
			msg.MultiContent = convertContentParts(m.Parts)
		} else {
			msg.Content = m.Content
		}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, goopenai.ToolCall{
				ID:   tc.ID,
				Type: goopenai.ToolType(tc.Type),
				Function: goopenai.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		messages = append(messages, msg)
	}

	goReq := goopenai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
	}

	if len(req.Tools) > 0 {
		goReq.Tools = convertTools(req.Tools)
	}

	if req.ToolChoice != nil {
		goReq.ToolChoice = req.ToolChoice
	}

	if req.MaxTokens > 0 {
		goReq.MaxCompletionTokens = req.MaxTokens
	}

	if req.Temperature != nil {
		goReq.Temperature = *req.Temperature
	}

	if req.TopP != nil {
		goReq.TopP = *req.TopP
	}

	return goReq
}

func convertContentParts(parts []provider.ContentPart) []goopenai.ChatMessagePart {
	result := make([]goopenai.ChatMessagePart, 0, len(parts))
	for _, p := range parts {
		switch part := p.(type) {
		case provider.TextPart:
			result = append(result, goopenai.ChatMessagePart{
				Type: goopenai.ChatMessagePartTypeText,
				Text: part.Text,
			})
		case provider.ImagePart:
			result = append(result, goopenai.ChatMessagePart{
				Type: goopenai.ChatMessagePartTypeImageURL,
				ImageURL: &goopenai.ChatMessageImageURL{
					URL: part.URL,
				},
			})
		}
	}
	return result
}

func convertTools(tools []map[string]any) []goopenai.Tool {
	result := make([]goopenai.Tool, 0, len(tools))
	for _, t := range tools {
		fn, _ := t["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params := fn["parameters"]

		tool := goopenai.Tool{
			Type: goopenai.ToolTypeFunction,
			Function: &goopenai.FunctionDefinition{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		}
		result = append(result, tool)
	}
	return result
}

func parseChatResponse(resp goopenai.ChatCompletionResponse) *ChatResponse {
	cr := &ChatResponse{}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		cr.Text = choice.Message.Content
		cr.Reasoning = choice.Message.ReasoningContent
		cr.Text, cr.Reasoning = stripThinkTags(cr.Text, cr.Reasoning)
		for _, tc := range choice.Message.ToolCalls {
			cr.ToolCalls = append(cr.ToolCalls, ToolCall{
				ID:   tc.ID,
				Type: string(tc.Type),
				Function: ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		// Fallback: some models emit tool calls as raw XML in the content field.
		if len(cr.ToolCalls) == 0 && cr.Text != "" {
			calls, cleaned := extractFallbackToolCalls(cr.Text)
			if len(calls) > 0 {
				cr.ToolCalls = calls
				cr.Text = cleaned
			}
		}
	}
	cr.Usage = &Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}
	return cr
}

func parseStreamChunk(resp goopenai.ChatCompletionStreamResponse) StreamChunk {
	chunk := StreamChunk{}
	if len(resp.Choices) > 0 {
		delta := resp.Choices[0].Delta
		chunk.Text = delta.Content
		chunk.Reasoning = delta.ReasoningContent
		for _, tc := range delta.ToolCalls {
			chunk.ToolCalls = append(chunk.ToolCalls, ToolCall{
				ID:   tc.ID,
				Type: string(tc.Type),
				Function: ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}
	if resp.Usage != nil {
		chunk.Usage = &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return chunk
}

// Inner returns the underlying go-openai client for advanced usage.
func (c *Client) Inner() *goopenai.Client {
	return c.inner
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	if c.config.BaseURL != "" {
		return c.config.BaseURL
	}
	return "https://api.openai.com/v1"
}
