package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/auth"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/provider/openai"
)

// ChatProvider is the interface that execution.go uses to talk to the LLM.
// It can be implemented by *openai.Client or a fake for testing.
type ChatProvider = provider.ChatProvider

const maxClientCache = 50

// LLMCore is the retry + fallback engine.
type LLMCore struct {
	model           string
	fallbacks       []string
	maxRetries      int
	apiKey          string
	apiBase         string
	verbose         int
	errorClassifier func(error) ErrorKind

	mu          sync.Mutex
	clientCache map[string]ChatProvider

	tokenURL     string
	clientID     string
	clientSecret string
	headers      map[string]string

	httpResponseHeaderTimeout time.Duration
	httpClientTimeout         time.Duration

	// ClientFactory can be overridden for testing.
	ClientFactory func(model string) ChatProvider
}

// LLMCoreConfig configures the LLMCore engine.
type LLMCoreConfig struct {
	Model           string
	FallbackModels  []string
	MaxRetries      int // default 3
	APIKey          string
	APIBase         string
	TokenURL        string // OAuth2 client_credentials token endpoint (optional)
	ClientID        string
	ClientSecret    string
	Verbose         int
	ErrorClassifier func(error) ErrorKind
	// Headers are additional HTTP headers sent with every request (e.g., User-Agent).
	Headers map[string]string
	// HTTPResponseHeaderTimeout is passed to the OpenAI-compatible HTTP client; zero uses openai package defaults.
	HTTPResponseHeaderTimeout time.Duration
	// HTTPClientTimeout caps the full HTTP round trip; zero uses openai package defaults.
	HTTPClientTimeout time.Duration
}

// NewLLMCore creates a new retry/fallback engine.
func NewLLMCore(cfg LLMCoreConfig) *LLMCore {
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	return &LLMCore{
		model:                     cfg.Model,
		fallbacks:                 cfg.FallbackModels,
		maxRetries:                cfg.MaxRetries,
		apiKey:                    cfg.APIKey,
		apiBase:                   cfg.APIBase,
		tokenURL:                  cfg.TokenURL,
		clientID:                  cfg.ClientID,
		clientSecret:              cfg.ClientSecret,
		headers:                   cfg.Headers,
		httpResponseHeaderTimeout: cfg.HTTPResponseHeaderTimeout,
		httpClientTimeout:         cfg.HTTPClientTimeout,
		verbose:                   cfg.Verbose,
		errorClassifier:           cfg.ErrorClassifier,
		clientCache:               make(map[string]ChatProvider),
	}
}

// RunChatOpts holds the parameters for a chat request through the engine.
type RunChatOpts struct {
	Messages     []map[string]any
	Tools        []map[string]any
	ToolChoice   any
	MaxTokens    int
	Temperature  *float32
	TopP         *float32
	ExtraHeaders map[string]string
}

// RunChat executes a chat with retry + fallback logic.
func (c *LLMCore) RunChat(ctx context.Context, opts RunChatOpts) (*provider.ChatResponse, error) {
	models := append([]string{c.model}, c.fallbacks...)
	var lastErr error

	for _, model := range models {
		client := c.GetClient(model)
		req := c.buildChatRequest(model, opts)

		if c.verbose >= 1 {
			slog.Info("LLM request", "model", model, "messages", len(req.Messages), "tools", len(req.Tools))
		}
		if c.verbose >= 3 {
			if data, err := json.MarshalIndent(req, "", "  "); err == nil {
				slog.Debug("request body", "body", string(data))
			}
		}

		for attempt := range c.maxRetries + 1 {
			if c.verbose >= 2 && attempt > 0 {
				slog.Debug("retry attempt", "attempt", attempt, "model", model)
			}
			resp, err := client.ChatCompletion(ctx, req)
			if err == nil {
				if c.verbose >= 1 {
					toolCallCount := 0
					if resp.ToolCalls != nil {
						toolCallCount = len(resp.ToolCalls)
					}
					if resp.Usage != nil {
						slog.Info("LLM response", "text_len", len(resp.Text), "tool_calls", toolCallCount,
							"prompt_tokens", resp.Usage.PromptTokens, "completion_tokens", resp.Usage.CompletionTokens, "total_tokens", resp.Usage.TotalTokens)
					} else {
						slog.Info("LLM response", "text_len", len(resp.Text), "tool_calls", toolCallCount)
					}
				}
				if c.verbose >= 3 {
					if data, err := json.MarshalIndent(resp, "", "  "); err == nil {
						slog.Debug("response body", "body", string(data))
					}
				}
				return resp, nil
			}
			lastErr = err
			kind := c.ClassifyError(err)

			if c.verbose >= 1 {
				slog.Info("LLM error", "error", err, "kind", kind)
			}

			// Non-retryable errors: skip retries, try fallback
			if kind != ErrTemporary {
				break
			}
			if attempt < c.maxRetries {
				backoff := time.Duration(1<<attempt)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
	}

	if lastErr != nil {
		kind := c.ClassifyError(lastErr)
		return nil, NewError(kind, lastErr.Error(), lastErr)
	}
	return nil, NewError(ErrUnknown, "all models exhausted", nil)
}

// RunChatStream executes a streaming chat with retry + fallback logic.
func (c *LLMCore) RunChatStream(ctx context.Context, opts RunChatOpts) (<-chan provider.StreamChunk, error) {
	models := append([]string{c.model}, c.fallbacks...)
	var lastErr error

	for _, model := range models {
		client := c.GetClient(model)
		req := c.buildChatRequest(model, opts)
		req.Stream = true
		req.StreamOptions = &provider.StreamOptions{IncludeUsage: true}

		for attempt := range c.maxRetries + 1 {
			ch, err := client.ChatCompletionStream(ctx, req)
			if err == nil {
				return ch, nil
			}
			lastErr = err
			kind := c.ClassifyError(err)
			if kind != ErrTemporary {
				break
			}
			if attempt < c.maxRetries {
				backoff := time.Duration(1<<attempt)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
	}

	if lastErr != nil {
		kind := c.ClassifyError(lastErr)
		return nil, NewError(kind, lastErr.Error(), lastErr)
	}
	return nil, NewError(ErrUnknown, "all models exhausted", nil)
}

// ClassifyError classifies an error using a 4-layer strategy.
func (c *LLMCore) ClassifyError(err error) ErrorKind {
	if c.errorClassifier != nil {
		return c.errorClassifier(err)
	}
	return c.defaultClassifyError(err)
}

func (c *LLMCore) defaultClassifyError(err error) ErrorKind {
	// Layer 1: check for HTTP status code
	if code := extractHTTPStatus(err); code > 0 {
		return classifyByHTTPStatus(code)
	}

	// Layer 2: text matching
	return classifyByTextSignature(err)
}

func extractHTTPStatus(err error) int {
	type statusCoder interface {
		StatusCode() int
	}
	if sc, ok := err.(statusCoder); ok {
		return sc.StatusCode()
	}

	// Check for status_code field via interface
	type statusCodeField interface {
		HTTPStatusCode() int
	}
	if sc, ok := err.(statusCodeField); ok {
		return sc.HTTPStatusCode()
	}

	return 0
}

func classifyByHTTPStatus(code int) ErrorKind {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return ErrProvider
	case code == http.StatusTooManyRequests:
		return ErrTemporary
	case code == http.StatusBadRequest:
		return ErrInvalidInput
	case code == http.StatusNotFound:
		return ErrNotFound
	case code >= 500:
		return ErrProvider
	default:
		return ErrUnknown
	}
}

func classifyByTextSignature(err error) ErrorKind {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests"):
		return ErrTemporary
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out"):
		return ErrTemporary
	case strings.Contains(msg, "connection") && (strings.Contains(msg, "refused") || strings.Contains(msg, "reset")):
		return ErrTemporary
	case strings.Contains(msg, "invalid") || strings.Contains(msg, "validation") ||
		strings.Contains(msg, "exceeded limit") || strings.Contains(msg, "input length"):
		return ErrInvalidInput
	case strings.Contains(msg, "not found"):
		return ErrNotFound
	case strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden"):
		return ErrProvider
	default:
		return ErrUnknown
	}
}

// GetClient returns (or creates + caches) a ChatProvider for the given model.
func (c *LLMCore) GetClient(model string) ChatProvider {
	c.mu.Lock()
	if client, ok := c.clientCache[model]; ok {
		c.mu.Unlock()
		return client
	}
	c.mu.Unlock()

	var client ChatProvider
	if c.ClientFactory != nil {
		client = c.ClientFactory(model)
	} else {
		oc := openai.ClientConfig{
			APIKey:                    c.apiKey,
			BaseURL:                   c.apiBase,
			Headers:                   c.headers,
			HTTPResponseHeaderTimeout: c.httpResponseHeaderTimeout,
			HTTPClientTimeout:         c.httpClientTimeout,
		}
		if c.tokenURL != "" && c.clientID != "" && c.clientSecret != "" {
			oc.APIKey = ""
			oc.BearerToken = auth.ClientCredentialsTokenSource(c.tokenURL, c.clientID, c.clientSecret)
		}
		client = openai.NewClient(oc)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.clientCache[model]; ok {
		return existing
	}
	if len(c.clientCache) >= maxClientCache {
		// Evict a random entry to keep cache bounded
		for k := range c.clientCache {
			delete(c.clientCache, k)
			break
		}
	}
	c.clientCache[model] = client
	return client
}

func (c *LLMCore) buildChatRequest(model string, opts RunChatOpts) provider.ChatRequest {
	messages := make([]provider.Message, 0, len(opts.Messages))
	for _, m := range opts.Messages {
		role := getString(m, "role")
		content := getString(m, "content")

		// Some providers (e.g. Kimi) reject assistant messages with empty content.
		// When an assistant message carries only tool_calls and no text, skip it
		// unless it actually has tool_calls attached (those are valid and required).
		if role == "assistant" && content == "" {
			if _, hasTC := m["tool_calls"]; !hasTC {
				continue
			}
		}

		msg := provider.Message{
			Role:    role,
			Content: content,
		}
		if toolCallID, ok := m["tool_call_id"].(string); ok {
			msg.ToolCallID = toolCallID
		}
		// Normalize tool_calls[].function.arguments to valid JSON string.
		// Tape history loaded via json.Unmarshal often yields `[]any` instead of
		// `[]map[string]any`, so we accept both forms.
		if rawToolCalls, ok := m["tool_calls"]; ok {
			forEachToolCall(rawToolCalls, func(tc map[string]any) {
				fn, _ := tc["function"].(map[string]any)
				var normalizedArgs string
				if fn != nil {
					normalizedArgs = normalizeFunctionArguments(fn["arguments"])
				}
				msg.ToolCalls = append(msg.ToolCalls, provider.ToolCall{
					ID:   getString(tc, "id"),
					Type: getString(tc, "type"),
					Function: provider.ToolCallFunction{
						Name:      getString(fn, "name"),
						Arguments: normalizedArgs,
					},
				})
			})
		}
		if role == "assistant" {
			if rc := getString(m, "reasoning"); rc != "" {
				msg.ReasoningContent = rc
			} else if rc := getString(m, "reasoning_content"); rc != "" {
				msg.ReasoningContent = rc
			}
		}
		messages = append(messages, msg)
	}

	req := provider.ChatRequest{
		Model:        model,
		Messages:     messages,
		Tools:        opts.Tools,
		ToolChoice:   opts.ToolChoice,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		TopP:         opts.TopP,
		ExtraHeaders: opts.ExtraHeaders,
	}
	return req
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func forEachToolCall(v any, fn func(map[string]any)) {
	switch tc := v.(type) {
	case []map[string]any:
		for _, item := range tc {
			fn(item)
		}
	case []any:
		for _, item := range tc {
			if m, ok := item.(map[string]any); ok {
				fn(m)
			}
		}
	}
}

// normalizeFunctionArguments ensures v becomes a JSON string suitable for
// tool/function calling payloads.
//
// DMR stores tool call arguments as a string in tape/history; different
// providers may produce non-strict JSON or even JSON-encoded strings.
// Some strict backends reject such payloads with "function.arguments must be
// in JSON format".
func normalizeFunctionArguments(v any) string {
	switch x := v.(type) {
	case nil:
		return "{}"
	case string:
		return normalizeArgumentsString(x)
	default:
		// If arguments is already a structured value (map/array),
		// normalize to a JSON object string.
		if m, ok := x.(map[string]any); ok {
			return string(mustMarshal(m))
		}
		// Fallback: wrap as {"raw": <value>}
		return string(mustMarshal(map[string]any{"raw": x}))
	}
}

func normalizeArgumentsString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}

	// We intentionally normalize to a JSON *object* string because some
	// strict backends reject non-object JSON (or JSON-string-wrapped JSON).
	// That matches picoclaw's approach: it always marshals from a map.
	var parsed any
	if err := json.Unmarshal([]byte(s), &parsed); err == nil {
		// Most common: already a JSON object
		if m, ok := parsed.(map[string]any); ok {
			if b, err2 := json.Marshal(m); err2 == nil {
				return string(b)
			}
		}

		// Special case: s is a JSON string that itself contains JSON.
		if inner, ok := parsed.(string); ok && inner != "" {
			var innerParsed any
			if err2 := json.Unmarshal([]byte(inner), &innerParsed); err2 == nil {
				if m, ok := innerParsed.(map[string]any); ok {
					if b, err3 := json.Marshal(m); err3 == nil {
						return string(b)
					}
				}
			}
		}

		// Fallback: wrap whatever parsed value into a JSON object.
		if b, err2 := json.Marshal(map[string]any{"raw": parsed}); err2 == nil {
			return string(b)
		}
	}

	// If it's not strict JSON at all, wrap raw string to keep the payload valid JSON.
	if b, err := json.Marshal(map[string]any{"raw": s}); err == nil {
		return string(b)
	}
	return `{"raw":""}`
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

// Model returns the primary model name.
func (c *LLMCore) Model() string {
	return c.model
}

// MaxRetries returns the max retry count.
func (c *LLMCore) MaxRetries() int {
	return c.maxRetries
}

// SetClientForModel directly sets a client for a model (useful for testing).
func (c *LLMCore) SetClientForModel(model string, client ChatProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clientCache[model] = client
}

// Verbose returns the verbosity level
func (c *LLMCore) Verbose() int {
	return c.verbose
}
