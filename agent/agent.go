// Package agent implements the Agent loop for DMR.
// It orchestrates multi-turn LLM conversations with automatic tool execution.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/seanly/dmr-devkit/agent/toolresult"
	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/observe"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/tools/handoff"
	"github.com/seanly/dmr-devkit/tools/toolsearch"
)

const defaultToolResultMaxChars = toolresult.DefaultMaxResultChars

// Config configures the Agent.
type Config struct {
	MaxSteps     int
	AgentPolicy  config.AgentConfig // YAML agent section: defaults for context handoff + completion cap resolution
	SystemPrompt string
	// SystemPromptBase is the resolved agent system prompt from config only (no plugin fragments).
	// When plugins register SystemPrompt hooks, the loop refreshes SystemPrompt from Base each LLM step.
	SystemPromptBase string
	// SystemPromptBases maps tape name glob patterns to resolved prompt strings.
	// Used by systemPromptBaseForTape() to select per-tape system prompts.
	SystemPromptBases map[string]string
	// TapeModels maps tape name glob patterns to model names for per-tape model selection.
	TapeModels  map[string]string
	Tools       []*tool.Tool
	Workspace   string
	Verbose     int
	Models      []config.ModelConfig
	OnToolCall  func(event ToolCallEvent) // optional callback for tool call display
	OnUIWidget  func(widget any)          // optional callback for A2UI widget payloads
	TapeControl any                       // plugin.TapeControl — injected by host
	DefaultTape string                    // canonical session tape for stable override keys

	// Tracer enables OpenTelemetry-aligned spans for agent, llm_call and tool_call
	// lifecycle events. When nil, no spans are recorded.
	Tracer *observe.Tracer

	// MaxDuplicateToolCalls limits how many times the same tool with the same
	// arguments may be executed within a single agent run. Zero disables the guard.
	MaxDuplicateToolCalls int
	// MaxTotalToolCalls limits the total number of tool calls in a single agent
	// run. Zero disables the guard.
	MaxTotalToolCalls int
}

// Agent orchestrates multi-turn LLM + tool execution.
type Agent struct {
	defaultChat *client.ChatClient // default ChatClient when no per-tape override
	tape        *tape.TapeManager
	hooks       Hooks
	config      Config
	executor    *tool.ToolExecutor

	// tapeStates holds all mutable per-tape runtime state under a single coherent lock.
	tapeStates tapeStateMap

	// cfgMu protects the two mutable config fields that can be injected after New.
	cfgMu sync.Mutex

	onToolCallMu      sync.RWMutex // protects config.OnToolCall
	reviewRunner      ReviewDelegate
	extendedTools     []*tool.Tool // cached extended tools from all plugins
	extLoaded         bool         // guarded by extMu
	extMu             sync.Mutex   // guards extendedTools and extLoaded

	toolResults *toolresult.Manager // large tool-output externalization + microcompact state

	// Precomputed sorted prompt bases and tape models for fast lookup
	precomputedPromptBases []struct{ pattern, prompt string }
	precomputedTapeModels  []struct{ pattern, model string }

	// builtinTools are the devkit-injected tools (toolSearch, handoff, etc.) that
	// are always available to the agent loop and may also be exposed to the host
	// for comma/slash command dispatch.
	builtinTools []*tool.Tool
}

// SetTapeControl injects the TapeControl dependency.
func (a *Agent) SetTapeControl(tc any) {
	a.cfgMu.Lock()
	a.config.TapeControl = tc
	a.cfgMu.Unlock()
}

// SetDefaultTape sets the canonical session tape name used by interceptors for
// stable override keys (e.g. CLI REPL so ,tape.switch resolves relative to the
// original session tape regardless of current effective tape).
func (a *Agent) SetDefaultTape(tape string) {
	a.cfgMu.Lock()
	a.config.DefaultTape = tape
	a.cfgMu.Unlock()
}

// New creates a new Agent.
// hooks may be nil for a minimal loop (no plugin extensions); otherwise pass an implementation
// such as *plugin.Manager from pkg/plugin.
func New(chat *client.ChatClient, tm *tape.TapeManager, hooks Hooks, cfg Config) *Agent {
	if cfg.MaxSteps == 0 {
		cfg.MaxSteps = 20
	}
	if cfg.MaxDuplicateToolCalls == 0 {
		cfg.MaxDuplicateToolCalls = 2
	}
	if cfg.MaxTotalToolCalls == 0 {
		cfg.MaxTotalToolCalls = 20
	}
	if hooks == nil {
		hooks = noopHooks{}
	}
	a := &Agent{
		defaultChat: chat,
		tape:        tm,
		hooks:       hooks,
		config:      cfg,
		tapeStates:  newTapeStateMap(maxChatClients),
		toolResults: toolresult.NewManager(buildToolResultPolicy(cfg)),
	}
	a.precomputePromptBases()
	a.precomputeTapeModels()

	// Inject built-in toolSearch for deferred tool discovery and handoff for focused compaction.
	// Copy config.Tools to avoid mutating the caller's slice.
	builtinTools := []*tool.Tool{toolsearch.NewTool(a), handoff.NewTool(a)}
	if len(a.config.Tools) > 0 {
		builtinTools = append(builtinTools, a.config.Tools...)
	}
	a.builtinTools = builtinTools[:2]
	a.config.Tools = builtinTools

	return a
}

// BuiltinTools returns the devkit-injected built-in tools (e.g. toolSearch, handoff).
// These are always loaded and may be exposed to the host for slash/comma command dispatch.
func (a *Agent) BuiltinTools() []*tool.Tool {
	out := make([]*tool.Tool, len(a.builtinTools))
	copy(out, a.builtinTools)
	return out
}

// SetOnToolCall sets the callback for tool call display.
func (a *Agent) SetOnToolCall(fn func(ToolCallEvent)) {
	a.onToolCallMu.Lock()
	a.config.OnToolCall = fn
	a.onToolCallMu.Unlock()
}

// SetOnUIWidget sets the callback for UI widget payloads (e.g. A2UI).
func (a *Agent) SetOnUIWidget(fn func(widget any)) {
	a.onToolCallMu.Lock()
	a.config.OnUIWidget = fn
	a.onToolCallMu.Unlock()
}

// EmitUIWidget invokes the configured OnUIWidget callback synchronously (if set).
// Policy hooks may use this inside BeforeToolCall to push confirmation UI before the runner
// unblocks — for example, workflow.AgentNode.RunEvents attaches OnUIWidget to stream A2UI to SSE clients.
func (a *Agent) EmitUIWidget(widget any) {
	a.onToolCallMu.RLock()
	fn := a.config.OnUIWidget
	a.onToolCallMu.RUnlock()
	if fn != nil {
		fn(widget)
	}
}

// Tracer returns the configured tracer, or nil.
func (a *Agent) Tracer() *observe.Tracer {
	return a.config.Tracer
}

// SetExecutor stores the tool executor reference for rebuilding chat clients.
func (a *Agent) SetExecutor(e *tool.ToolExecutor) {
	a.executor = e
}

func (a *Agent) precomputePromptBases() {
	patterns := make([]string, 0, len(a.config.SystemPromptBases))
	for p := range a.config.SystemPromptBases {
		patterns = append(patterns, p)
	}
	sort.Slice(patterns, func(i, j int) bool { return len(patterns[i]) > len(patterns[j]) })
	a.precomputedPromptBases = make([]struct{ pattern, prompt string }, 0, len(patterns))
	for _, p := range patterns {
		a.precomputedPromptBases = append(a.precomputedPromptBases, struct{ pattern, prompt string }{p, a.config.SystemPromptBases[p]})
	}
}

// systemPromptBaseForTape returns the base system prompt for a given tape name.
// Matches against SystemPromptBases glob patterns; falls back to SystemPromptBase.
// Patterns are sorted by length descending so the most specific match wins.
func (a *Agent) systemPromptBaseForTape(tapeName string) string {
	for _, entry := range a.precomputedPromptBases {
		if matched, _ := path.Match(entry.pattern, tapeName); matched {
			return entry.prompt
		}
	}
	return a.config.SystemPromptBase
}

func (a *Agent) precomputeTapeModels() {
	patterns := make([]string, 0, len(a.config.TapeModels))
	for p := range a.config.TapeModels {
		patterns = append(patterns, p)
	}
	sort.Slice(patterns, func(i, j int) bool { return len(patterns[i]) > len(patterns[j]) })
	a.precomputedTapeModels = make([]struct{ pattern, model string }, 0, len(patterns))
	for _, p := range patterns {
		a.precomputedTapeModels = append(a.precomputedTapeModels, struct{ pattern, model string }{p, a.config.TapeModels[p]})
	}
}

// modelNameForTape returns the model name for a given tape based on glob matching.
// Returns empty string if no match (caller should use default model).
// Patterns are sorted by length descending so the most specific match wins.
func (a *Agent) modelNameForTape(tapeName string) string {
	for _, entry := range a.precomputedTapeModels {
		if matched, _ := path.Match(entry.pattern, tapeName); matched {
			return entry.model
		}
	}
	return ""
}

// AllModels returns all configured models.
func (a *Agent) AllModels() []config.ModelConfig {
	return a.config.Models
}

// AllModelInfos returns all models as [ModelInfo] (implements [RuntimeAgent]).
func (a *Agent) AllModelInfos() []ModelInfo {
	infos := make([]ModelInfo, len(a.config.Models))
	for i, m := range a.config.Models {
		infos[i] = ModelInfo{Name: m.Name, Model: m.Model}
	}
	return infos
}

// GetCurrentModelName returns the name and model ID for the given tape (implements [RuntimeAgent]).
func (a *Agent) GetCurrentModelName(tapeName string) (string, string) {
	m := a.GetCurrentModel(tapeName)
	if m == nil {
		return "", ""
	}
	return m.Name, m.Model
}

// GetCurrentModel returns the model in use for the given tape.
func (a *Agent) GetCurrentModel(tapeName string) *config.ModelConfig {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	name := ts.modelOverride
	ts.mu.Unlock()

	if name != "" {
		for i := range a.config.Models {
			if a.config.Models[i].Name == name {
				return &a.config.Models[i]
			}
		}
	}
	for i := range a.config.Models {
		if a.config.Models[i].Default {
			return &a.config.Models[i]
		}
	}
	if len(a.config.Models) > 0 {
		return &a.config.Models[0]
	}
	return nil
}

// SwitchModel switches the model for the given tape (in-memory only).
// It also resolves skill model route hints via AgentConfig.SkillModels.
func (a *Agent) SwitchModel(tapeName, modelName string) error {
	if strings.TrimSpace(modelName) == "" {
		return core.NewError(core.ErrInvalidInput, "model name is empty", nil)
	}

	// Resolve skill model route hints (e.g. "cheap" → "gpt-4o-mini").
	if routed, ok := a.config.AgentPolicy.SkillModels[modelName]; ok {
		modelName = routed
	}

	for i := range a.config.Models {
		m := &a.config.Models[i]
		if m.Name == modelName || m.Model == modelName {
			cc := a.buildChatClient(m)
			ts := a.tapeStates.getOrCreate(tapeName)
			ts.mu.Lock()
			ts.modelOverride = m.Name
			ts.chatClient = cc
			ts.mu.Unlock()
			return nil
		}
	}
	return core.NewError(core.ErrConfig, fmt.Sprintf("model not found: %s", modelName), nil)
}

const maxChatClients = 100
const maxToolsCache = 100

func (a *Agent) storeChatClient(tapeName string, cc *client.ChatClient) {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.chatClient = cc
	ts.touch()
}

// RestartProcess sends SIGHUP to the current process to trigger a hot restart
// (config reload + plugin re-initialization) without killing the process.
// Falls back to SIGTERM if SIGHUP is not available.
func (a *Agent) RestartProcess() error {
	slog.Info("dmr: ,restart command — sending SIGHUP to self for hot restart")
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return p.Signal(syscall.SIGHUP)
}

// buildChatClient creates a new ChatClient for the given model config
func (a *Agent) buildChatClient(model *config.ModelConfig) *client.ChatClient {
	httpHdr, httpClient := model.HTTPTimeouts()
	llmCore := core.NewLLMCore(core.LLMCoreConfig{
		Model:                     model.Model,
		APIKey:                    model.APIKey,
		APIBase:                   model.APIBase,
		TokenURL:                  model.TokenURL,
		ClientID:                  model.ClientID,
		ClientSecret:              model.ClientSecret,
		Headers:                   model.Headers,
		HTTPResponseHeaderTimeout: httpHdr,
		HTTPClientTimeout:         httpClient,
		MaxRetries:                3,
		Verbose:                   a.config.Verbose,
	})
	return client.NewChatClient(llmCore, a.executor, a.tape)
}

// getChatClient returns the ChatClient for the given tape.
// If the tape has a model override, returns a per-tape client; otherwise returns the default client.
func (a *Agent) getChatClient(tapeName string) *client.ChatClient {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	cc := ts.chatClient
	modelName := ts.modelOverride
	ts.mu.Unlock()

	if cc != nil {
		return cc
	}

	if modelName != "" {
		for i := range a.config.Models {
			if a.config.Models[i].Name == modelName {
				cc = a.buildChatClient(&a.config.Models[i])
				a.storeChatClient(tapeName, cc)
				return cc
			}
		}
	}

	// Use default client
	return a.defaultChat
}

func (a *Agent) handoffContextLimit(tapeName string) int {
	m := a.GetCurrentModel(tapeName)
	if m == nil {
		return 0
	}
	return m.ResolveContextLimit(a.config.AgentPolicy)
}

// ContextTokenBudget returns the configured prompt-token budget used by proactive handoff
// (i.e. the same value as handoffContextLimit, exposed for UI telemetry).
func (a *Agent) ContextTokenBudget(tapeName string) int {
	return a.handoffContextLimit(tapeName)
}

// shouldCompactNow checks whether a compact is allowed at the given step.
// It enforces a minimum 3-step gap between compacts for the same tape, but
// relaxes that gap when estimated tokens have already crossed the configured
// handoff threshold (so rapid token growth after many tool calls can still be
// compacted). It resets if the step counter wraps (new conversation cycle).
func (a *Agent) shouldCompactNow(tapeName string, step, estimatedTokens int, limit int, threshold float64) bool {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	defer ts.mu.Unlock()

	lastCompact := ts.lastCompactStep
	hasCompacted := lastCompact >= 0
	// If current step < last recorded step, it's a new conversation cycle — reset.
	if hasCompacted && step < lastCompact {
		ts.lastCompactStep = 0
		return true
	}

	gap := 0
	if hasCompacted {
		gap = step - lastCompact
	}

	// Normal rule: allow if never compacted or at least 3 steps have passed.
	if !hasCompacted || gap >= 3 {
		return true
	}

	// Pressure override: if the context is already above the handoff threshold,
	// allow compaction after just one step so tool-heavy runs don't overflow
	// while waiting for the normal 3-step cooldown.
	if limit > 0 && estimatedTokens > 0 && gap >= 1 {
		if float64(estimatedTokens) >= float64(limit)*threshold {
			return true
		}
	}

	return false
}

// canHandoffTool checks whether the built-in handoff tool is allowed to run on
// the given tape. It prevents the LLM from invoking handoff repeatedly in short
// succession when there has been no meaningful new conversation since the last
// anchor (handoff or compact). The check is intentionally conservative: it only
// blocks the LLM-driven handoff tool; user-initiated slash commands and automatic
// loop-level handoffs are not gated here.
func (a *Agent) CanHandoffTool(tapeName string) bool {
	entries, err := a.tape.Store.FetchAll(tapeName, nil)
	if err != nil {
		return true
	}

	// Find the most recent anchor (handoff or compact).
	lastAnchorIdx := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "anchor" {
			lastAnchorIdx = i
			break
		}
	}

	// No prior anchor — allow the first handoff.
	if lastAnchorIdx < 0 {
		return true
	}

	// Count meaningful entries after the last anchor.
	const minEntriesSinceAnchor = 5
	count := 0
	for i := lastAnchorIdx + 1; i < len(entries); i++ {
		switch entries[i].Kind {
		case "message", "tool_call", "tool_result":
			count++
		}
	}

	return count >= minEntriesSinceAnchor
}

// recordCompactStep records that a compact occurred at the given step.
func (a *Agent) recordCompactStep(tapeName string, step int) {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	ts.lastCompactStep = step
	ts.mu.Unlock()
	a.persistTapeState(tapeName)
}

func (a *Agent) handoffThreshold(tapeName string) float64 {
	m := a.GetCurrentModel(tapeName)
	if m == nil {
		return 0.8
	}
	return m.ResolveHandoffThreshold(a.config.AgentPolicy)
}

func (a *Agent) completionMaxTokensForTape(tapeName string) int {
	m := a.GetCurrentModel(tapeName)
	if m == nil {
		return 0
	}
	return m.ResolveCompletionMaxTokens(a.config.AgentPolicy)
}

// toolResultMaxCharsForTape resolves the effective externalize threshold (runes)
// for role="tool" content for a given tape.
//
// Priority:
//  1. model.ToolResultMaxChars (0=unset, -1=disable externalize)
//  2. agent.AgentPolicy.ToolResultMaxChars (0=unset, -1=disable)
//  3. Auto-calculated based on max_token and handoff_threshold
//  4. defaultToolResultMaxChars (50_000)
func (a *Agent) toolResultMaxCharsForTape(tapeName string) int {
	m := a.GetCurrentModel(tapeName)

	// 1. If user explicitly configured, use it
	if m != nil && m.ToolResultMaxChars != 0 {
		return m.ToolResultMaxChars
	}
	if a.config.AgentPolicy.ToolResultMaxChars != 0 {
		return a.config.AgentPolicy.ToolResultMaxChars
	}

	// 2. Auto-calculate based on max_token and handoff_threshold
	if m != nil && m.MaxToken > 0 {
		threshold := m.ResolveHandoffThreshold(a.config.AgentPolicy)

		// Calculation logic:
		// - When handoff triggers, history occupies max_token * threshold
		// - Space left for tool result = max_token * (1 - threshold)
		// - Reserve 20% safety margin
		// - 1 token ≈ 4 chars (may vary by language)
		availableTokens := int(float64(m.MaxToken) * (1 - threshold) * 0.8)
		maxChars := availableTokens * 4

		slog.Info("auto-calculated tool_result_max_chars", "model", m.Name, "max_chars", maxChars, "max_token", m.MaxToken, "threshold", threshold)

		return maxChars
	}

	// 3. Fallback to default
	return defaultToolResultMaxChars
}

// shouldAutoHandoff checks if prompt_tokens exceed the configured threshold for this tape's model.
func (a *Agent) shouldAutoHandoff(tapeName string, latestUsage map[string]any) bool {
	limit := a.handoffContextLimit(tapeName)
	if limit <= 0 || latestUsage == nil {
		return false
	}
	pt, ok := intFromUsageMap(latestUsage, "prompt_tokens")
	if !ok {
		return false
	}
	a.contextBudgetForTape(tapeName).UpdateReported(pt)
	th := a.handoffThreshold(tapeName)
	return float64(pt) >= float64(limit)*th
}

// shouldAutoHandoffByEstimate checks if estimated tokens exceed the threshold.
// This is used for preemptive compact before calling the API.
func (a *Agent) shouldAutoHandoffByEstimate(tapeName string, estimatedTokens int) bool {
	limit := a.handoffContextLimit(tapeName)
	if limit <= 0 || estimatedTokens <= 0 {
		return false
	}
	a.contextBudgetForTape(tapeName).UpdateEstimated(estimatedTokens)
	th := a.handoffThreshold(tapeName)
	return float64(estimatedTokens) >= float64(limit)*th
}

// contextBudgetForTape returns the context budget tracker for the given tape.
func (a *Agent) contextBudgetForTape(tapeName string) *contextBudget {
	return a.tapeStates.getOrCreate(tapeName).budget
}

// estimateContextTokens estimates the token count for the current tape context.
// Returns 0 if estimation fails.
func (a *Agent) estimateContextTokens(tapeName string, tapeCtx *tape.TapeContext) int {
	// Read messages from tape
	messages, err := a.tape.ReadMessages(tapeName, tapeCtx)
	if err != nil {
		slog.Warn("failed to read messages for token estimation", "error", err)
		return 0
	}

	if len(messages) == 0 {
		return 0
	}

	// Use TokenEstimator for estimation
	estimator := NewTokenEstimator()
	estimatedTokens := estimator.Estimate(messages)

	slog.Debug("estimated context tokens", "tape", tapeName, "tokens", estimatedTokens)
	return estimatedTokens
}

func intFromUsageMap(u map[string]any, key string) (int, bool) {
	v, ok := u[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// ========== Tool Discovery Methods ==========

// IsToolDiscovered checks if a deferred tool has been discovered for the tape.
func (a *Agent) IsToolDiscovered(tapeName, toolName string) bool {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	discovered := ts.discoveredTools[toolName]
	ts.mu.Unlock()
	return discovered
}

// DiscoverTool marks a tool as discovered for the tape.
func (a *Agent) DiscoverTool(tapeName, toolName string) {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	ts.discoveredTools[toolName] = true
	ts.toolsCache = nil
	ts.mu.Unlock()
	if a.config.Verbose >= 1 {
		slog.Info("agent: tool discovered", "tool", toolName, "tape", tapeName)
	}
	a.persistTapeState(tapeName)
}

// GetAllCoreTools returns all core tools from plugins (cached).
func (a *Agent) GetAllCoreTools() []*tool.Tool {
	return a.hooks.CollectAllTools(context.Background(), true, false)
}

// GetAllExtendedTools returns all extended tools from plugins (cached).
func (a *Agent) GetAllExtendedTools() []*tool.Tool {
	a.extMu.Lock()
	defer a.extMu.Unlock()

	if !a.extLoaded {
		a.extendedTools = a.hooks.CollectAllTools(context.Background(), false, true)
		a.extLoaded = true
		if a.config.Verbose >= 1 {
			slog.Info("agent: loaded extended tools", "count", len(a.extendedTools))
		}
	}

	return a.extendedTools
}

// InvalidateExtendedTools clears the extended-tool cache so the next discovery
// reloads tools from plugins (e.g. after a local bridge worker connects).
func (a *Agent) InvalidateExtendedTools() {
	a.extMu.Lock()
	a.extLoaded = false
	a.extendedTools = nil
	a.extMu.Unlock()

	a.tapeStates.rangeLocked(func(name string, ts *tapeState) {
		ts.mu.Lock()
		ts.toolsCache = nil
		ts.mu.Unlock()
	})

	if a.config.Verbose >= 1 {
		slog.Info("agent: invalidated extended tools cache")
	}
}

// SearchTools searches for extended tools matching the query.
func (a *Agent) SearchTools(query string) []*tool.Tool {
	extended := a.GetAllExtendedTools()
	return tool.SearchTools(extended, query)
}

// Resume attempts to resume a pending execution on the given tape.
// It replays the execution history and re-triggers the agent loop if the
// execution is still pending and the agent ID has not changed.
func (a *Agent) Resume(ctx context.Context, tapeName string) (*Result, error) {
	tc := tape.NewTapeController(a.tape)

	execID, err := tc.FindPendingExec(tapeName)
	if err != nil {
		return nil, fmt.Errorf("find pending exec: %w", err)
	}
	if execID == "" {
		return nil, nil // nothing to resume
	}

	replay, err := tc.ReplayExec(tapeName, execID)
	if err != nil {
		return nil, fmt.Errorf("replay exec: %w", err)
	}

	// Check agent ID consistency
	currentModel := a.GetCurrentModel(tapeName)
	currentAgentID := ""
	if currentModel != nil {
		currentAgentID = currentModel.Name
	}
	if replay.AgentID != "" && replay.AgentID != currentAgentID {
		return nil, fmt.Errorf("resumption rejected: agent changed from %s to %s", replay.AgentID, currentAgentID)
	}

	slog.Info("resuming pending execution", "tape", tapeName, "exec", execID, "state", replay.State)

	// Re-trigger the agent loop with a continuation prompt.
	// The tape already contains the user's original message, so we just
	// ask the agent to continue processing.
	return a.Run(ctx, tapeName, "Continue processing.", 0)
}

// Handoff creates an anchor and clears discovered tools for the tape.
// This should be used instead of tape.Handoff() to ensure tool discovery
// state is properly reset on handoff.
func (a *Agent) Handoff(tapeName, name string, state map[string]any) {
	a.ClearDiscoveredTools(tapeName)
	if err := a.hooks.OnContextReset(context.Background(), tapeName, "handoff"); err != nil {
		slog.Warn("OnContextReset failed", "tape", tapeName, "reason", "handoff", "error", err)
	}
	if _, err := a.tape.Handoff(tapeName, name, state); err != nil {
		slog.Warn("tape handoff failed", "name", name, "error", err)
	}
}

// ClearDiscoveredTools clears all discovered tool state for a tape.
// This resets the tool discovery state, requiring tools to be re-discovered
// via toolSearch before they can be used again.
func (a *Agent) ClearDiscoveredTools(tapeName string) {
	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	count := len(ts.discoveredTools)
	ts.discoveredTools = make(map[string]bool)
	ts.toolsCache = nil
	ts.mu.Unlock()

	if a.config.Verbose >= 1 {
		slog.Info("agent: cleared discovered tools", "count", count, "tape", tapeName)
	}

	// Notify hooks so they can reset per-tape state (e.g. search counters)
	_ = a.hooks.OnDiscoveredToolsCleared(context.Background(), tapeName)
	a.persistTapeState(tapeName)
}

// isChildTape reports whether tapeName belongs to a transient subagent run.
func isChildTape(tapeName string) bool {
	return strings.Contains(tapeName, ":"+SubagentTapeSuffix+":")
}

// persistTapeState writes the current tapeState to an audit-only agent_state entry.
func (a *Agent) persistTapeState(tapeName string) {
	if a.tape == nil || a.tape.Store == nil || isChildTape(tapeName) {
		return
	}

	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	discovered := make([]string, 0, len(ts.discoveredTools))
	for name := range ts.discoveredTools {
		discovered = append(discovered, name)
	}
	sort.Strings(discovered)
	lastCompact := ts.lastCompactStep
	modelOverride := ts.modelOverride
	ts.mu.Unlock()

	payload := map[string]any{
		"discovered_tools":  discovered,
		"last_compact_step": lastCompact,
		"model_override":    modelOverride,
	}
	if err := a.tape.AppendEntry(tapeName, tape.NewAgentStateEntry(payload)); err != nil {
		slog.Warn("agent: failed to persist tape state", "tape", tapeName, "error", err)
		return
	}
	slog.Debug("agent: persisted tape state", "tape", tapeName, "discovered_tools", len(discovered), "last_compact_step", lastCompact, "model_override", modelOverride)
}

// restoreTapeState scans the tape for the latest agent_state entry and restores
// discovered tools, last compact step, and model override. It rebuilds the chat
// client when a model override is present.
func (a *Agent) restoreTapeState(tapeName string) {
	if a.tape == nil || a.tape.Store == nil || isChildTape(tapeName) {
		return
	}

	entries, err := a.tape.Store.FetchAll(tapeName, nil)
	if err != nil {
		slog.Warn("agent: failed to fetch tape for state restore", "tape", tapeName, "error", err)
		return
	}

	var latest map[string]any
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "agent_state" {
			latest = entries[i].Payload
			break
		}
	}
	if latest == nil {
		return
	}

	ts := a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	if dt, ok := latest["discovered_tools"].([]any); ok {
		ts.discoveredTools = make(map[string]bool, len(dt))
		for _, v := range dt {
			if s, ok := v.(string); ok {
				ts.discoveredTools[s] = true
			}
		}
		ts.toolsCache = nil
	} else if dt, ok := latest["discovered_tools"].([]string); ok {
		ts.discoveredTools = make(map[string]bool, len(dt))
		for _, s := range dt {
			ts.discoveredTools[s] = true
		}
		ts.toolsCache = nil
	}

	switch v := latest["last_compact_step"].(type) {
	case float64:
		ts.lastCompactStep = int(v)
	case int:
		ts.lastCompactStep = v
	case int64:
		ts.lastCompactStep = int(v)
	}

	modelOverride := ""
	if mo, ok := latest["model_override"].(string); ok {
		modelOverride = mo
	}
	ts.mu.Unlock()

	if modelOverride != "" {
		if err := a.SwitchModel(tapeName, modelOverride); err != nil {
			slog.Warn("agent: failed to restore model override", "tape", tapeName, "model", modelOverride, "error", err)
		}
	}
}
