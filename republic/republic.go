// Package republic is a tape-first LLM client library for Go.
// It records all messages, tool calls, errors, and usage as structured data
// in an append-only audit trail.
//
// DMR (Decide, Monitor, Review): "In dmr we trust, but verify."
// - Decide: Let AI make intelligent decisions with tool calling
// - Monitor: Record everything in Tape audit trail
// - Review: Verify AI decisions through structured data
package republic

import (
	"context"
	"log/slog"
	"time"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/provider/openai"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// Re-export types for convenience.
type (
	ErrorKind      = core.ErrorKind
	RepublicError  = core.RepublicError
	ErrorPayload   = core.ErrorPayload
	StreamEvent    = core.StreamEvent
	ToolAutoResult = core.ToolAutoResult
	ToolCallData   = core.ToolCallData
	ToolExecution  = core.ToolExecution

	TapeEntry   = tape.TapeEntry
	TapeStore   = tape.TapeStore
	TapeContext = tape.TapeContext
	TapeQuery   = tape.TapeQuery

	Tool        = tool.Tool
	ToolSet     = tool.ToolSet
	ToolContext = tool.ToolContext
)

// Re-export constants.
const (
	ErrInvalidInput = core.ErrInvalidInput
	ErrConfig       = core.ErrConfig
	ErrProvider     = core.ErrProvider
	ErrTool         = core.ErrTool
	ErrTemporary    = core.ErrTemporary
	ErrNotFound     = core.ErrNotFound
	ErrUnknown      = core.ErrUnknown
)

// Config configures the LLM facade.
type Config struct {
	Model           string
	APIKey          string
	BaseURL         string
	FallbackModels  []string
	MaxRetries      int
	Verbose         int
	TapeStore       tape.TapeStore
	Context         *tape.TapeContext
	ErrorClassifier func(error) core.ErrorKind
	// Headers are additional HTTP headers sent with every request (e.g., User-Agent).
	Headers map[string]string
	// HTTPResponseHeaderTimeout and HTTPClientTimeout are passed to the OpenAI-compatible HTTP client; zero uses openai package defaults.
	HTTPResponseHeaderTimeout time.Duration
	HTTPClientTimeout         time.Duration
}

// LLM is the main entry point for Republic.
type LLM struct {
	chat      *client.ChatClient
	text      *client.TextClient
	embedding *openai.Client
	tape      *tape.TapeManager
	config    Config
	llmCore   *core.LLMCore
}

// New creates a new LLM instance.
func New(cfg Config) *LLM {
	if cfg.MaxRetries == 0 && cfg.Model != "" {
		cfg.MaxRetries = 3
	}

	// Setup tape store
	var store tape.TapeStore
	if cfg.TapeStore != nil {
		store = cfg.TapeStore
	} else {
		store = tape.NewInMemoryTapeStore()
	}
	tm := tape.NewTapeManager(store)

	// Setup LLM core
	llmCore := core.NewLLMCore(core.LLMCoreConfig{
		Model:                     cfg.Model,
		FallbackModels:            cfg.FallbackModels,
		MaxRetries:                cfg.MaxRetries,
		APIKey:                    cfg.APIKey,
		APIBase:                   cfg.BaseURL,
		Headers:                   cfg.Headers,
		HTTPResponseHeaderTimeout: cfg.HTTPResponseHeaderTimeout,
		HTTPClientTimeout:         cfg.HTTPClientTimeout,
		Verbose:                   cfg.Verbose,
		ErrorClassifier:           cfg.ErrorClassifier,
	})

	// Setup executor
	executor := tool.NewToolExecutor()

	// Setup clients
	chatClient := client.NewChatClient(llmCore, executor, tm)
	textClient := client.NewTextClient(chatClient)

	// Setup embedding client
	embeddingClient := openai.NewClient(openai.ClientConfig{
		APIKey:                    cfg.APIKey,
		BaseURL:                   cfg.BaseURL,
		HTTPResponseHeaderTimeout: cfg.HTTPResponseHeaderTimeout,
		HTTPClientTimeout:         cfg.HTTPClientTimeout,
	})

	return &LLM{
		chat:      chatClient,
		text:      textClient,
		embedding: embeddingClient,
		tape:      tm,
		config:    cfg,
		llmCore:   llmCore,
	}
}

// ChatOption is a functional option for chat methods.
type ChatOption func(*client.ChatOpts)

func WithTools(tools ...*tool.Tool) ChatOption {
	return func(o *client.ChatOpts) { o.Tools = tools }
}

func WithToolChoice(choice any) ChatOption {
	return func(o *client.ChatOpts) { o.ToolChoice = choice }
}

func WithToolContext(ctx *tool.ToolContext) ChatOption {
	return func(o *client.ChatOpts) { o.ToolContext = ctx }
}

func WithSystemPrompt(prompt string) ChatOption {
	return func(o *client.ChatOpts) { o.SystemPrompt = prompt }
}

func WithMaxTokens(n int) ChatOption {
	return func(o *client.ChatOpts) { o.MaxTokens = n }
}

func WithTemperature(t float32) ChatOption {
	return func(o *client.ChatOpts) { o.Temperature = &t }
}

func WithMessages(msgs []map[string]any) ChatOption {
	return func(o *client.ChatOpts) { o.Messages = msgs }
}

func WithTape(name string) ChatOption {
	return func(o *client.ChatOpts) { o.Tape = name }
}

func WithContext(ctx *tape.TapeContext) ChatOption {
	return func(o *client.ChatOpts) { o.Context = ctx }
}

func WithMaxToolRounds(n int) ChatOption {
	return func(o *client.ChatOpts) { o.MaxToolRounds = n }
}

func WithExtraHeaders(headers map[string]string) ChatOption {
	return func(o *client.ChatOpts) { o.ExtraHeaders = headers }
}

func applyOpts(prompt string, opts []ChatOption) client.ChatOpts {
	o := client.ChatOpts{Prompt: prompt}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Chat performs a non-streaming chat.
func (l *LLM) Chat(ctx context.Context, prompt string, opts ...ChatOption) (string, error) {
	return l.chat.Chat(ctx, applyOpts(prompt, opts))
}

// ToolCalls performs a chat and returns the tool calls.
func (l *LLM) ToolCalls(ctx context.Context, prompt string, opts ...ChatOption) ([]core.ToolCallData, error) {
	return l.chat.ToolCalls(ctx, applyOpts(prompt, opts))
}

// RunTools performs a chat and automatically executes tool calls.
func (l *LLM) RunTools(ctx context.Context, prompt string, opts ...ChatOption) (*core.ToolAutoResult, error) {
	return l.chat.RunTools(ctx, applyOpts(prompt, opts))
}

// Stream performs a streaming chat, returning a channel of text chunks.
func (l *LLM) Stream(ctx context.Context, prompt string, opts ...ChatOption) (<-chan string, *client.StreamState, error) {
	return l.chat.Stream(ctx, applyOpts(prompt, opts))
}

// StreamEvents performs a streaming chat, returning a channel of structured events.
func (l *LLM) StreamEvents(ctx context.Context, prompt string, opts ...ChatOption) (<-chan core.StreamEvent, *client.StreamState, error) {
	return l.chat.StreamEvents(ctx, applyOpts(prompt, opts))
}

// If asks the LLM a yes/no question about the input.
func (l *LLM) If(ctx context.Context, input, question string) (bool, error) {
	return l.text.If(ctx, input, question)
}

// Classify asks the LLM to classify the input.
func (l *LLM) Classify(ctx context.Context, input string, choices []string) (string, error) {
	return l.text.Classify(ctx, input, choices)
}

// Embed creates embeddings for the given inputs.
func (l *LLM) Embed(ctx context.Context, model string, inputs []string) (*provider.EmbedResponse, error) {
	return l.embedding.Embedding(ctx, provider.EmbedRequest{
		Model: model,
		Input: inputs,
	})
}

// Tape returns a TapeSession for the given tape name.
func (l *LLM) Tape(name string) *TapeSession {
	return &TapeSession{
		llm:   l,
		name:  name,
		Query: tape.NewQuery(name, l.tape.Store),
	}
}

// Core returns the underlying LLMCore (for testing/advanced usage).
func (l *LLM) Core() *core.LLMCore {
	return l.llmCore
}

// TapeSession is a scoped view of a tape.
type TapeSession struct {
	llm   *LLM
	name  string
	Query *tape.TapeQuery
}

// Chat performs a chat scoped to this tape.
func (s *TapeSession) Chat(ctx context.Context, prompt string, opts ...ChatOption) (string, error) {
	opts = append(opts, WithTape(s.name))
	if s.llm.config.Context != nil {
		opts = append(opts, WithContext(s.llm.config.Context))
	} else {
		opts = append(opts, WithContext(tape.NewLastAnchorContext()))
	}
	return s.llm.Chat(ctx, prompt, opts...)
}

// Handoff creates an anchor in this tape.
func (s *TapeSession) Handoff(name string, state map[string]any) {
	if _, err := s.llm.tape.Handoff(s.name, name, state); err != nil {
		slog.Warn("tape handoff failed", "name", name, "error", err)
	}
}

// Stream performs a streaming chat scoped to this tape.
func (s *TapeSession) Stream(ctx context.Context, prompt string, opts ...ChatOption) (<-chan string, *client.StreamState, error) {
	opts = append(opts, WithTape(s.name))
	if s.llm.config.Context != nil {
		opts = append(opts, WithContext(s.llm.config.Context))
	} else {
		opts = append(opts, WithContext(tape.NewLastAnchorContext()))
	}
	return s.llm.Stream(ctx, prompt, opts...)
}
