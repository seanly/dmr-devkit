// Package devkit wires a runnable [agent.Agent] with minimal boilerplate for embedded
// or experimental Go tools. It is not a replacement for the full CLI ([cmd/dmr]) with
// config files and the plugin ecosystem.
package devkit

import (
	"context"
	"fmt"
	"os"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/observe"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// DefaultTapeName is a conventional single-session tape name for [Kit.Agent.Run] in local / CLI-style use.
// For an A2A HTTP server (package a2aserver), the default is one tape per A2A Task, not this constant.
const DefaultTapeName = "default"

// Kit holds a fully wired agent and its dependencies for advanced use.
type Kit struct {
	Agent *agent.Agent
	// TapeManager is the same instance used by Agent and Client.
	TapeManager *tape.TapeManager
	// Store is the backing tape store (e.g. in-memory or file).
	Store  tape.TapeStore
	Client *client.ChatClient
	// Hooks is the extension surface used for this kit (same instance passed to [agent.New] when non-nil).
	Hooks agent.Hooks

	onClose func(context.Context) error
}

// Close runs OnClose from [Options] if set (e.g. plugin manager shutdown).
func (k *Kit) Close(ctx context.Context) error {
	if k == nil || k.onClose == nil {
		return nil
	}
	return k.onClose(ctx)
}

// Options configures [Build]. Zero values use in-memory tape, built-in default system
// prompt, and agent max steps from [agent.New] (20 when MaxSteps is 0).
type Options struct {
	// Model is the provider model id (required), e.g. "gpt-4o".
	Model string
	// APIKey is sent as the bearer token when not using OAuth client_credentials.
	APIKey  string
	APIBase string

	// TokenURL, ClientID, ClientSecret enable OAuth2 client_credentials (optional).
	// When set, APIBase is required; APIKey may be empty.
	TokenURL     string
	ClientID     string
	ClientSecret string
	Headers      map[string]string

	HTTPResponseHeaderTimeout int
	HTTPClientTimeout         int

	// ModelName is the logical name for this model in multi-model APIs (optional).
	// Defaults to "default" when empty.
	ModelName string

	// Models, when non-empty, registers multiple model endpoints on the agent.
	// The first entry with Default=true, or the first entry overall, is the primary LLM.
	// When set, Model/APIKey/APIBase are ignored for Build unless Models is empty.
	Models []config.ModelConfig

	Verbose int

	// Workspace is passed through to [agent.Config] and, when non-empty, to tape file
	// store config if TapeConfig.Workspace is unset.
	Workspace string

	// SystemPromptExtra is appended after the built-in default system prompt (same idea
	// as config agent.system_prompt). Ignored when SystemPromptBase is set.
	SystemPromptExtra string
	// SystemPromptBase, when non-empty, replaces the usual default+extra merge and
	// becomes the base before [agent.Hooks.ComposeSystemPrompt] runs.
	SystemPromptBase string

	MaxSteps int

	// MaxDuplicateToolCalls limits repeated identical tool calls within a single
	// agent run. Zero uses the agent default (2).
	MaxDuplicateToolCalls int
	// MaxTotalToolCalls limits the total tool calls within a single agent run.
	// Zero uses the agent default (20).
	MaxTotalToolCalls int

	// AgentPolicy is optional policy (handoff, token limits, etc.).
	AgentPolicy config.AgentConfig

	// Tools are registered as core tools on the agent.
	Tools []*tool.Tool

	// Hooks extends the agent loop (policy, tools from plugins, prompt fragments, etc.).
	// When nil, a no-op implementation is used. For full DMR behavior, pass *plugin.Manager
	// (it implements [agent.Hooks]) from your main package together with OnClose.
	Hooks agent.Hooks

	// OnClose is invoked from [Kit.Close] (optional). Use it to shut down a plugin manager
	// or other resources allocated outside devkit.
	OnClose func(context.Context) error

	// TapeStore, if non-nil, is used as-is.
	TapeStore tape.TapeStore
	// TapeConfig is used when TapeStore is nil and at least one of Driver, DSN, or Dir
	// is set; otherwise an in-memory store is used.
	TapeConfig tape.StoreConfig

	// TapeTimezone is passed to [tape.SetTimezone] when non-empty (optional).
	TapeTimezone string

	// Tracer enables OpenTelemetry-aligned spans. When nil, no spans are recorded.
	Tracer *observe.Tracer
}

func (o *Options) modelConfig() config.ModelConfig {
	name := o.ModelName
	if name == "" {
		name = "default"
	}
	m := config.ModelConfig{
		Name:                      name,
		Model:                     o.Model,
		APIKey:                    o.APIKey,
		APIBase:                   o.APIBase,
		Default:                   true,
		TokenURL:                  o.TokenURL,
		ClientID:                  o.ClientID,
		ClientSecret:              o.ClientSecret,
		Headers:                   o.Headers,
		HTTPResponseHeaderTimeout: o.HTTPResponseHeaderTimeout,
		HTTPClientTimeout:         o.HTTPClientTimeout,
	}
	return m
}

func (o *Options) resolvedModels() []config.ModelConfig {
	if len(o.Models) > 0 {
		out := make([]config.ModelConfig, len(o.Models))
		copy(out, o.Models)
		hasDefault := false
		for i := range out {
			if out[i].Default {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			out[0].Default = true
		}
		for i := range out {
			if out[i].Name == "" {
				out[i].Name = out[i].Model
			}
			if out[i].Name == "" {
				out[i].Name = fmt.Sprintf("model%d", i)
			}
		}
		return out
	}
	return []config.ModelConfig{o.modelConfig()}
}

func (o *Options) validate() error {
	if len(o.Models) > 0 {
		for i := range o.Models {
			if err := validateModelConfig(&o.Models[i]); err != nil {
				return fmt.Errorf("devkit: models[%d]: %w", i, err)
			}
		}
		return nil
	}
	if o.Model == "" {
		return fmt.Errorf("devkit: Model is required")
	}
	return validateModelConfig(&config.ModelConfig{
		Model:        o.Model,
		APIKey:       o.APIKey,
		APIBase:      o.APIBase,
		TokenURL:     o.TokenURL,
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
	})
}

func validateModelConfig(m *config.ModelConfig) error {
	if m == nil || m.Model == "" {
		return fmt.Errorf("model is required")
	}
	switch {
	case m.TokenURL != "" || m.ClientID != "" || m.ClientSecret != "":
		if m.TokenURL == "" || m.ClientID == "" || m.ClientSecret == "" {
			return fmt.Errorf("token_url, client_id, and client_secret must all be set for OAuth")
		}
		if m.APIBase == "" {
			return fmt.Errorf("api_base is required when using OAuth client_credentials")
		}
	case m.APIKey == "":
		return fmt.Errorf("api_key is required, or set token_url, client_id, and client_secret")
	default:
	}
	return nil
}

func (o *Options) openTapeStore() (tape.TapeStore, error) {
	if o.TapeStore != nil {
		return o.TapeStore, nil
	}
	if o.TapeConfig.Driver != "" || o.TapeConfig.DSN != "" || o.TapeConfig.Dir != "" {
		cfg := o.TapeConfig
		if cfg.Workspace == "" && o.Workspace != "" {
			cfg.Workspace = o.Workspace
		}
		return tape.NewStore(cfg)
	}
	return tape.NewInMemoryTapeStore(), nil
}

func wireExecutor(hooks agent.Hooks, verbose int) *tool.ToolExecutor {
	ex := tool.NewToolExecutor()
	ex.Verbose = verbose
	ex.BeforeToolCall = func(ctx context.Context, t *tool.Tool, args map[string]any, toolCtx *tool.ToolContext) error {
		return hooks.BeforeToolCall(ctx, t, args, toolCtx)
	}
	ex.BatchBeforeToolCall = func(ctx context.Context, items []tool.BatchCheckItem) map[int]error {
		return hooks.BatchBeforeToolCall(ctx, items)
	}
	return ex
}

// Build constructs a [Kit] from options: tape store, LLM core, optional hooks, chat client,
// and agent loop.
func Build(ctx context.Context, opts Options) (*Kit, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	store, err := opts.openTapeStore()
	if err != nil {
		return nil, fmt.Errorf("devkit: tape store: %w", err)
	}

	if opts.TapeTimezone != "" {
		if tzErr := tape.SetTimezone(opts.TapeTimezone); tzErr != nil {
			return nil, fmt.Errorf("devkit: tape timezone: %w", tzErr)
		}
	}

	hooks := opts.Hooks
	if hooks == nil {
		hooks = agent.NopHooks()
	}

	models := opts.resolvedModels()
	mc := models[0]
	httpHdr, httpClient := mc.HTTPTimeouts()
	llmCore := core.NewLLMCore(core.LLMCoreConfig{
		Model:                     mc.Model,
		APIKey:                    mc.APIKey,
		APIBase:                   mc.APIBase,
		TokenURL:                  mc.TokenURL,
		ClientID:                  mc.ClientID,
		ClientSecret:              mc.ClientSecret,
		Headers:                   mc.Headers,
		HTTPResponseHeaderTimeout: httpHdr,
		HTTPClientTimeout:         httpClient,
		MaxRetries:                3,
		Verbose:                   opts.Verbose,
	})

	tm := tape.NewTapeManager(store)
	ex := wireExecutor(hooks, opts.Verbose)
	chat := client.NewChatClient(llmCore, ex, tm)

	var finalBase string
	switch {
	case opts.SystemPromptBase != "":
		finalBase = opts.SystemPromptBase
	default:
		finalBase = config.DefaultSystemPrompt()
		if opts.SystemPromptExtra != "" {
			finalBase = finalBase + "\n\n---\n\n" + opts.SystemPromptExtra
		}
	}
	systemPrompt := hooks.ComposeSystemPrompt(ctx, finalBase)

	maxSteps := opts.MaxSteps
	if maxSteps == 0 && opts.AgentPolicy.MaxSteps > 0 {
		maxSteps = opts.AgentPolicy.MaxSteps
	}

	agCfg := agent.Config{
		MaxSteps:              maxSteps,
		MaxDuplicateToolCalls: opts.MaxDuplicateToolCalls,
		MaxTotalToolCalls:     opts.MaxTotalToolCalls,
		AgentPolicy:           opts.AgentPolicy,
		SystemPrompt:          systemPrompt,
		SystemPromptBase:      finalBase,
		Tools:                 opts.Tools,
		Workspace:             opts.Workspace,
		Verbose:               opts.Verbose,
		Models:                models,
		Tracer:                opts.Tracer,
	}

	ag := agent.New(chat, tm, hooks, agCfg)
	ag.SetExecutor(ex)

	return &Kit{
		Agent:       ag,
		TapeManager: tm,
		Store:       store,
		Client:      chat,
		Hooks:       hooks,
		onClose:     opts.OnClose,
	}, nil
}

// EnvOptions returns Options with Model, APIKey, and APIBase filled from the usual
// environment variables (AI_MODEL, AI_API_KEY, AI_API_BASE). Empty vars are skipped.
func EnvOptions() Options {
	o := Options{
		Model:   os.Getenv("AI_MODEL"),
		APIKey:  os.Getenv("AI_API_KEY"),
		APIBase: os.Getenv("AI_API_BASE"),
	}
	return o
}
