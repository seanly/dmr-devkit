// Package config holds embed-oriented configuration types shared by dmr-devkit
// (no top-level Config with plugins — that lives in the github.com/seanly/dmr product repo).
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// FTS5Mode represents the SQLite FTS5 enable mode: true, false, or "auto".
type FTS5Mode struct {
	value string // "true", "false", "auto"
}

// FTS5True, FTS5False, FTS5Auto are the canonical FTS5Mode values.
var (
	FTS5True  = FTS5Mode{value: "true"}
	FTS5False = FTS5Mode{value: "false"}
	FTS5Auto  = FTS5Mode{value: "auto"}
)

// NewFTS5Mode creates an FTS5Mode from a string. Unrecognized values default to "false".
func NewFTS5Mode(s string) FTS5Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return FTS5True
	case "auto", "automatic":
		return FTS5Auto
	default:
		return FTS5False
	}
}

// String returns the canonical string representation.
func (m FTS5Mode) String() string {
	if m.value == "" {
		return "false"
	}
	return m.value
}

// IsTrue returns whether FTS5 is explicitly enabled.
func (m FTS5Mode) IsTrue() bool { return m.value == "true" }

// IsAuto returns whether FTS5 is in auto-detect mode.
func (m FTS5Mode) IsAuto() bool { return m.value == "auto" }

// IsFalse returns whether FTS5 is disabled.
func (m FTS5Mode) IsFalse() bool { return m.value == "" || m.value == "false" }

// UnmarshalText implements encoding.TextUnmarshaler for go-toml/v2 deserialization.
// Accepts string values: "true"/"false"/"auto"/"yes"/"no"/"on"/"off"/"1"/"0".
func (m *FTS5Mode) UnmarshalText(text []byte) error {
	*m = NewFTS5Mode(string(text))
	return nil
}

// UnmarshalTOML implements the go-toml/v2 Unmarshaler interface.
// Accepts bool (true/false) or string ("true"/"false"/"auto").
func (m *FTS5Mode) UnmarshalTOML(fn func(any) error) error {
	var raw any
	if err := fn(&raw); err != nil {
		return err
	}
	switch val := raw.(type) {
	case bool:
		if val {
			*m = FTS5True
		} else {
			*m = FTS5False
		}
	case string:
		*m = NewFTS5Mode(val)
	case int64:
		if val != 0 {
			*m = FTS5True
		} else {
			*m = FTS5False
		}
	default:
		return fmt.Errorf("enable_fts5: unsupported type %T", val)
	}
	return nil
}

// MarshalText implements encoding.TextMarshaler for TOML serialization.
func (m FTS5Mode) MarshalText() ([]byte, error) {
	return []byte(m.String()), nil
}

// CompactStrategy selects how tape entries are turned into LLM messages.
type CompactStrategy struct {
	value string // "summary", "snip", "collapse", "hybrid"
}

var (
	// CompactStrategySummary keeps the existing behavior (identity transform).
	// It is the zero value of CompactStrategy.
	CompactStrategySummary = CompactStrategy{}
	// CompactStrategySnip drops empty messages and duplicate system prompts.
	CompactStrategySnip = CompactStrategy{value: "snip"}
	// CompactStrategyCollapse merges adjacent same-role messages.
	CompactStrategyCollapse = CompactStrategy{value: "collapse"}
	// CompactStrategyHybrid applies snip then collapse.
	CompactStrategyHybrid = CompactStrategy{value: "hybrid"}
)

// NewCompactStrategy creates a CompactStrategy from a string. Unrecognized values default to Summary.
func NewCompactStrategy(s string) CompactStrategy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "snip":
		return CompactStrategySnip
	case "collapse":
		return CompactStrategyCollapse
	case "hybrid":
		return CompactStrategyHybrid
	default:
		return CompactStrategySummary
	}
}

// String returns the canonical string representation.
func (c CompactStrategy) String() string {
	if c.value == "" {
		return "summary"
	}
	return c.value
}

// IsSummary returns whether the strategy is the default summary/identity transform.
func (c CompactStrategy) IsSummary() bool { return c.value == "" || c.value == "summary" }

// IsSnip returns whether the strategy is snip.
func (c CompactStrategy) IsSnip() bool { return c.value == "snip" }

// IsCollapse returns whether the strategy is collapse.
func (c CompactStrategy) IsCollapse() bool { return c.value == "collapse" }

// IsHybrid returns whether the strategy is hybrid.
func (c CompactStrategy) IsHybrid() bool { return c.value == "hybrid" }

// MarshalText implements encoding.TextMarshaler.
func (c CompactStrategy) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (c *CompactStrategy) UnmarshalText(text []byte) error {
	*c = NewCompactStrategy(string(text))
	return nil
}

// UnmarshalTOML implements the go-toml/v2 Unmarshaler interface.
func (c *CompactStrategy) UnmarshalTOML(fn func(any) error) error {
	var raw any
	if err := fn(&raw); err != nil {
		return err
	}
	switch val := raw.(type) {
	case string:
		*c = NewCompactStrategy(val)
	default:
		return fmt.Errorf("compact_strategy: unsupported type %T", val)
	}
	return nil
}

// SystemPromptValue supports both a plain string and a list of file paths.
type SystemPromptValue struct {
	Raw   string   // direct string value
	Files []string // file path list
}

// UnmarshalTOML implements unstable.Unmarshaler (go-toml/v2) for direct string values.
// File path arrays are handled by [UnmarshalDocument] during config load.
func (s *SystemPromptValue) UnmarshalTOML(data []byte) error {
	var wrapper struct {
		V string `toml:"v"`
	}
	if err := toml.Unmarshal(append([]byte("v = "), data...), &wrapper); err != nil {
		return fmt.Errorf("system_prompt: %w", err)
	}
	s.Raw = wrapper.V
	s.Files = nil
	return nil
}

// MarshalTOML implements the go-toml/v2 Marshaler interface.
func (s SystemPromptValue) MarshalTOML() ([]byte, error) {
	if len(s.Files) > 0 {
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, f := range s.Files {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(strconv.Quote(f))
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	}
	return []byte(strconv.Quote(s.Raw)), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for single string values.
func (s *SystemPromptValue) UnmarshalText(text []byte) error {
	s.Raw = string(text)
	s.Files = nil
	return nil
}

// MarshalText implements encoding.TextMarshaler for single string values.
func (s SystemPromptValue) MarshalText() ([]byte, error) {
	return []byte(s.Raw), nil
}

// Resolve returns the final system prompt string.
// For file lists, reads each file relative to baseDir and joins with "\n\n".
func (s *SystemPromptValue) Resolve(baseDir string) (string, error) {
	if len(s.Files) == 0 {
		return s.Raw, nil
	}
	var parts []string
	for _, f := range s.Files {
		path := f
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read prompt file %s: %w", f, err)
		}
		parts = append(parts, strings.TrimSpace(string(data)))
	}
	return strings.Join(parts, "\n\n"), nil
}

// SystemPromptEntry defines a single per-tape system prompt configuration.
type SystemPromptEntry struct {
	Tape         string           `toml:"tape" json:"tape"`
	Profile      string           `toml:"profile,omitempty" json:"profile,omitempty"`
	SystemPrompt SystemPromptValue `toml:"system_prompt,omitempty" json:"system_prompt,omitempty"`
}

// ModelConfig configures a single model endpoint.
type ModelConfig struct {
	Name             string  `toml:"name"             json:"name"`
	Model            string  `toml:"model"            json:"model"`
	APIKey           string  `toml:"api_key"          json:"api_key"`
	APIBase          string  `toml:"api_base"         json:"api_base"`
	Default          bool    `toml:"default"          json:"default"`
	MaxToken         int     `toml:"max_token"        json:"max_token"` // context token budget for proactive handoff (vs usage.prompt_tokens); 0 = use agent default
	HandoffThreshold float64 `toml:"handoff_threshold" json:"handoff_threshold"`
	// CompletionMaxTokens is passed to the API as max completion tokens (OpenAI max_tokens / max_completion_tokens). 0 = omit (provider default).
	CompletionMaxTokens int `toml:"completion_max_tokens" json:"completion_max_tokens"`
	// ToolResultMaxChars configures when a single tool result is externalized to disk
	// before being sent back as role="tool" content:
	//
	//   - >0  : values longer than N runes trigger workspace persistence (+ preview tag)
	//   - -1  : never externalize; send full output (risk: context overflow)
	//   - 0   : unset; fall back to agent policy, model policy, auto-calc, then default (~50k)
	ToolResultMaxChars int `toml:"tool_result_max_chars" json:"tool_result_max_chars"`
	// TokenURL, ClientID, ClientSecret enable OAuth2 client_credentials against tokenURL
	// (e.g. Keycloak .../protocol/openid-connect/token) for Authorization Bearer on each LLM request.
	// Use with api_base pointing at an OpenAI-compatible proxy (e.g. LiteLLM). Implies api_key is optional.
	TokenURL     string `toml:"token_url"     json:"token_url"`
	ClientID     string `toml:"client_id"     json:"client_id"`
	ClientSecret string `toml:"client_secret" json:"client_secret"`
	// Headers are additional HTTP headers sent with every request (e.g., User-Agent, X-Client-Name).
	Headers map[string]string `toml:"headers" json:"headers,omitempty"`
	// HTTPResponseHeaderTimeout is seconds to wait for HTTP response headers after the request is sent.
	// 0 = use default (10 minutes), suitable for slow LLM APIs.
	HTTPResponseHeaderTimeout int `toml:"http_response_header_timeout" json:"http_response_header_timeout"`
	// HTTPClientTimeout is seconds for the entire HTTP request (headers + reading body).
	// 0 = use default (15 minutes). If set shorter than http_response_header_timeout, the client raises it.
	HTTPClientTimeout int `toml:"http_client_timeout" json:"http_client_timeout"`
	// SupportsVision indicates this model supports multi-modal (image) input.
	// When false, the system will not send image content parts to this model.
	// Defaults to true (most modern models support vision).
	VisionEnabled *bool `toml:"supports_vision" json:"supports_vision,omitempty"`
}

// HTTPTimeouts returns optional per-model HTTP timeouts for the OpenAI-compatible client.
// Zero values mean use provider/openai package defaults for header/body timeouts.
func (m *ModelConfig) HTTPTimeouts() (responseHeader, clientTotal time.Duration) {
	if m == nil {
		return 0, 0
	}
	if m.HTTPResponseHeaderTimeout > 0 {
		responseHeader = time.Duration(m.HTTPResponseHeaderTimeout) * time.Second
	}
	if m.HTTPClientTimeout > 0 {
		clientTotal = time.Duration(m.HTTPClientTimeout) * time.Second
	}
	return responseHeader, clientTotal
}

// UsesClientCredentials reports whether this model uses OAuth2 client_credentials instead of api_key.
func (m *ModelConfig) UsesClientCredentials() bool {
	return m.TokenURL != "" && m.ClientID != "" && m.ClientSecret != ""
}

// OAuthClientCredentialsIncomplete is true when only some of token_url / client_id / client_secret are set.
func (m *ModelConfig) OAuthClientCredentialsIncomplete() bool {
	n := 0
	if m.TokenURL != "" {
		n++
	}
	if m.ClientID != "" {
		n++
	}
	if m.ClientSecret != "" {
		n++
	}
	return n > 0 && n < 3
}

// ResolveContextLimit returns the prompt-token budget for proactive handoff (model overrides agent).
func (m *ModelConfig) ResolveContextLimit(agentCfg AgentConfig) int {
	if m.MaxToken > 0 {
		return m.MaxToken
	}
	return agentCfg.MaxToken
}

// ResolveCompletionMaxTokens returns API completion cap (model overrides agent). 0 means do not set.
func (m *ModelConfig) ResolveCompletionMaxTokens(agentCfg AgentConfig) int {
	if m.CompletionMaxTokens > 0 {
		return m.CompletionMaxTokens
	}
	return agentCfg.CompletionMaxTokens
}

// ResolveHandoffThreshold returns the effective threshold (model-level takes precedence).
func (m *ModelConfig) ResolveHandoffThreshold(agentCfg AgentConfig) float64 {
	if m.HandoffThreshold > 0 {
		return m.HandoffThreshold
	}
	if agentCfg.HandoffThreshold > 0 {
		return agentCfg.HandoffThreshold
	}
	return 0.75
}

// SupportsVision returns true if this model supports multi-modal image input.
// Defaults to true when unset (most modern models support vision).
// Configure via [[models]] supports_vision = false in TOML.
func (m *ModelConfig) SupportsVision() bool {
	if m == nil || m.VisionEnabled == nil {
		return true
	}
	return *m.VisionEnabled
}

// ToolResultMicrocompactConfig clears older compactable tool outputs on the wire before LLM requests.
type ToolResultMicrocompactConfig struct {
	Enabled          *bool    `toml:"enabled"`      // nil = not configured (use defaults); true/false = explicit
	KeepRecent       int      `toml:"keep_recent"`
	CompactableTools []string `toml:"compactable_tools"`
	GapMinutes       float64  `toml:"gap_minutes"`     // wall-clock gap since last assistant reply; 0 disables time trigger
	MaxAgeTurns      int      `toml:"max_age_turns"`   // clear tool results older than N assistant turns; 0 disables
	SizeThreshold    int      `toml:"size_threshold"`  // immediate externalize single results larger than this many chars; 0 disables
}

// ToolResultPolicyConfig configures externalized tool payloads and aggregate budgets.
type ToolResultPolicyConfig struct {
	DefaultMaxChars  int    `toml:"default_max_chars"`
	PerMessageBudget int    `toml:"per_message_budget"`
	PreviewChars     int    `toml:"preview_chars"`
	PersistSubdir    string `toml:"persist_subdir"`
	SkipTools        []string `toml:"skip_tools"`
	Microcompact     ToolResultMicrocompactConfig `toml:"microcompact"`
}

// ToolPersistenceConfig controls which discovered tools survive a compact/handoff.
// When nil or unset, behavior falls back to scaffolding profile (legacy/minimal keep,
// standard clears). Explicit fields override the profile.
type ToolPersistenceConfig struct {
	// ClearOnCompact disables preserving discovered tools across compacts.
	// nil = follow scaffolding profile; true = always clear; false = preserve.
	ClearOnCompact *bool `toml:"clear_on_compact,omitempty" json:"clear_on_compact,omitempty"`
	// KeepExtended preserves discovered extended tools unless marked Ephemeral.
	KeepExtended *bool `toml:"keep_extended,omitempty" json:"keep_extended,omitempty"`
	// KeepMCP preserves discovered MCP tools unless marked Ephemeral.
	KeepMCP *bool `toml:"keep_mcp,omitempty" json:"keep_mcp,omitempty"`
	// MaxDiscoveredTools caps the number of persisted discovered tools; 0 = unlimited.
	MaxDiscoveredTools int `toml:"max_discovered_tools,omitempty" json:"max_discovered_tools,omitempty"`
}

// ContextConfig controls how the context window is built after compacts.
type ContextConfig struct {
	// PersistSystemPrompt writes the composed system prompt as a regular "system"
	// entry (legacy). When false (default), it is written as an audit-only
	// "system_prompt" entry and supplied directly via ChatOpts.SystemPrompt.
	PersistSystemPrompt bool `toml:"persist_system_prompt,omitempty" json:"persist_system_prompt,omitempty"`
	// SoftBoundary keeps N raw messages before the last anchor as a safety net.
	SoftBoundary bool `toml:"soft_boundary,omitempty" json:"soft_boundary,omitempty"`
	// KeepBeforeAnchor is how many raw messages to retain before the last anchor.
	KeepBeforeAnchor int `toml:"keep_before_anchor,omitempty" json:"keep_before_anchor,omitempty"`
	// RollingSummary enables incremental summary updates instead of re-summarizing everything.
	RollingSummary bool `toml:"rolling_summary,omitempty" json:"rolling_summary,omitempty"`
	// QualityFallback falls back to raw-message retention when compact summary quality is poor.
	QualityFallback bool `toml:"quality_fallback,omitempty" json:"quality_fallback,omitempty"`
	// SnipCompact enables lightweight snip/collapse cleanup before LLM summarization.
	SnipCompact bool `toml:"snip_compact,omitempty" json:"snip_compact,omitempty"`
	// Strategy selects how tape entries are transformed into LLM messages.
	// Valid values: summary, snip, collapse, hybrid. Empty defaults to summary.
	Strategy CompactStrategy `toml:"compact_strategy" json:"compact_strategy,omitempty"`
}

// AgentConfig configures the agent loop.
type AgentConfig struct {
	MaxSteps            int     `toml:"max_steps"`
	MaxToken            int     `toml:"max_token"` // context budget for proactive handoff; 0 = disabled
	HandoffThreshold    float64 `toml:"handoff_threshold,omitempty"`
	CompletionMaxTokens int     `toml:"completion_max_tokens,omitempty"` // 0 = omit in API requests
	ToolResultMaxChars  int     `toml:"tool_result_max_chars,omitempty"`
	// ToolResultPolicy configures persisting large tool outputs and microcompaction.
	ToolResultPolicy ToolResultPolicyConfig `toml:"tool_result_policy,omitempty"`
	// ToolPersistence controls which discovered tools survive compact/handoff.
	ToolPersistence *ToolPersistenceConfig `toml:"tool_persistence,omitempty" json:"tool_persistence,omitempty"`
	// SystemPrompt can be either a string or array of strings (file paths)
	SystemPrompt SystemPromptValue `toml:"system_prompt,omitempty"`
	// SystemPrompts is a list of per-tape system prompt entries.
	// Each entry has a tape glob, optional profile, and optional system_prompt.
	SystemPrompts []SystemPromptEntry `toml:"system_prompts,omitempty"`
	// TapeModels maps tape name glob patterns to model names for per-tape model selection.
	// Example: "feishu_bot_ops:*" = "gpt-4o-mini"
	TapeModels map[string]string `toml:"models,omitempty"`
	// SkillModels maps skill model route hints to actual model names.
	// Example: "cheap" = "gpt-4o-mini", "reasoning" = "o3-mini"
	SkillModels map[string]string `toml:"skill_models,omitempty"`
	// Handoff configures structured task state and compact ordering.
	Handoff HandoffConfig `toml:"handoff,omitempty"`
	// Review configures post-tool adversarial review chains (critic skills).
	Review ReviewConfig `toml:"review,omitempty"`
	// Scaffolding selects harness profile (legacy|standard|minimal).
	Scaffolding ScaffoldingConfig `toml:"scaffolding,omitempty"`
	// Context controls context-window construction after compacts.
	Context ContextConfig `toml:"context" json:"context,omitempty"`
}

// HandoffConfig controls TaskState snapshots and compact ordering.
type HandoffConfig struct {
	StateEnabled          *bool  `toml:"state_enabled,omitempty"`
	CompactAfterState     bool   `toml:"compact_after_state"`
	CompactRequired       bool   `toml:"compact_required"`
	StateUpdate           string `toml:"state_update"` // heuristic | llm_extract
	MaxArtifacts          int    `toml:"max_artifacts"`
	MaxActiveFiles        int    `toml:"max_active_files"`
	CompactSummaryVersion int    `toml:"compact_summary_version,omitempty"`
}

// StateEnabledOrDefault returns whether structured task state is on (default true).
func (h HandoffConfig) StateEnabledOrDefault() bool {
	if h.StateEnabled != nil {
		return *h.StateEnabled
	}
	return true
}

// DefaultHandoffConfig returns harness defaults.
func DefaultHandoffConfig() HandoffConfig {
	enabled := true
	return HandoffConfig{
		StateEnabled:          &enabled,
		CompactAfterState:     true,
		CompactRequired:       false,
		StateUpdate:           "llm_extract",
		MaxArtifacts:          20,
		MaxActiveFiles:        10,
		CompactSummaryVersion: 1,
	}
}

// ReviewConfig configures automatic critic delegation after tools.
type ReviewConfig struct {
	Enabled              bool     `toml:"enabled"`
	AfterTools           []string `toml:"after_tools"`
	AfterToolPatterns    []string `toml:"after_tool_patterns"`
	Chain                []string `toml:"chain"`
	BlockOnCritical      bool     `toml:"block_on_critical"`
	MaxChainDepth        int      `toml:"max_chain_depth"`
	JudgeModel           string   `toml:"judge_model"`
}

// ScaffoldingConfig selects harness verbosity profile.
type ScaffoldingConfig struct {
	Profile string `toml:"profile"` // legacy | standard | minimal
}

// ResolveSystemPrompts resolves all file-based system prompts relative to baseDir.
// Returns a map of glob pattern → resolved prompt string.
func (a *AgentConfig) ResolveSystemPrompts(baseDir string) (map[string]string, error) {
	result := make(map[string]string)
	for _, entry := range a.SystemPrompts {
		resolved, err := entry.SystemPrompt.Resolve(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve system_prompts[%q]: %w", entry.Tape, err)
		}
		result[entry.Tape] = resolved
	}
	return result, nil
}

// TapeConfig configures the tape storage backend.
type TapeConfig struct {
	Driver         string   `toml:"driver"`                    // mem, file, sqlite, pg, mysql
	DSN            string   `toml:"dsn,omitempty"`             // database connection string
	Dir            string   `toml:"dir,omitempty"`             // file driver directory
	EnableTSVector bool     `toml:"enable_tsvector,omitempty"` // enable PostgreSQL full-text search (pg only)
	TSVectorLang   string   `toml:"tsvector_lang,omitempty"`   // tsvector language config (default: simple, auto-detect multilingual)
	Timezone       string   `toml:"timezone,omitempty"`        // IANA timezone for tape timestamps (e.g., "Asia/Shanghai", "UTC"). If empty, uses system local timezone.
	EnableFTS5     FTS5Mode `toml:"enable_fts5,omitempty"`     // enable SQLite FTS5: true/false/"auto" (default: true)
}

// DefaultDir returns the default config directory (~/.dmr).
func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dmr")
}

// DefaultPath returns the preferred default config file path (~/.dmr/config.toml).
func DefaultPath() string {
	return filepath.Join(DefaultDir(), "config.toml")
}

// ExistingDefaultConfigPath returns the first existing default config file under ~/.dmr.
// Only looks for config.toml (YAML support removed).
func ExistingDefaultConfigPath() (path string, ok bool) {
	dir := DefaultDir()
	p := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}

// EffectiveDefaultConfigPath returns the path for implicit config file operations.
func EffectiveDefaultConfigPath() string {
	if p, ok := ExistingDefaultConfigPath(); ok {
		return p
	}
	return DefaultPath()
}
