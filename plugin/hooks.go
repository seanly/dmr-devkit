package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
)

// RegistryHooks implements agent.Hooks by querying a capability Registry.
// It is the canonical way to wire capability-based plugins into the agent loop.
type RegistryHooks struct {
	registry      *Registry
	ErrorHandler  func(error)
}

// NewRegistryHooks creates an agent.Hooks implementation backed by the given Registry.
func NewRegistryHooks(r *Registry) agent.Hooks {
	return &RegistryHooks{registry: r}
}

// WithErrorHandler returns a new RegistryHooks that reports non-fatal errors
// from plugin hooks to the given handler. This is useful for surfacing
// plugin failures that would otherwise be silently ignored (e.g. a
// SystemPromptProvider returning an error).
func (h *RegistryHooks) WithErrorHandler(fn func(error)) *RegistryHooks {
	h.ErrorHandler = fn
	return h
}

func (h *RegistryHooks) reportf(format string, args ...any) {
	if h.ErrorHandler != nil {
		h.ErrorHandler(fmt.Errorf(format, args...))
	}
}

// ComposeSystemPrompt collects fragments from all SystemPromptProviders.
func (h *RegistryHooks) ComposeSystemPrompt(ctx context.Context, base string) string {
	if h.registry == nil {
		return base
	}
	var extras []string
	for _, sp := range h.registry.SystemPromptProviders() {
		frag, err := sp.SystemPrompt(ctx, base)
		if err != nil {
			h.reportf("plugin %q SystemPrompt error: %w", sp.(Plugin).Name(), err)
			continue
		}
		if t := strings.TrimSpace(frag); t != "" {
			extras = append(extras, t)
		}
	}
	if len(extras) == 0 {
		return base
	}
	joined := strings.Join(extras, "\n\n")
	if strings.TrimSpace(base) == "" {
		return joined
	}
	return strings.TrimSpace(base) + "\n\n" + joined
}

// CollectAllTools collects tools from all ToolProviders.
// When includeCore is true, tools with IsCore() == true are included.
// When includeExtended is true, tools with IsDeferred() == true are included.
func (h *RegistryHooks) CollectAllTools(ctx context.Context, includeCore, includeExtended bool) []*tool.Tool {
	if h.registry == nil {
		return nil
	}
	var out []*tool.Tool
	for _, tp := range h.registry.ToolProviders() {
		tools, err := tp.ListTools(ctx)
		if err != nil {
			h.reportf("plugin %q ListTools error: %w", tp.(Plugin).Name(), err)
			continue
		}
		for _, t := range tools {
			if t == nil {
				continue
			}
			if includeCore && t.IsCore() {
				out = append(out, t)
			} else if includeExtended && t.IsDeferred() {
				out = append(out, t)
			}
		}
	}
	return out
}

// AfterAgentRun calls all LifecycleHandlers.
func (h *RegistryHooks) AfterAgentRun(ctx context.Context, args agent.AfterAgentRunArgs) error {
	if h.registry == nil {
		return nil
	}
	for _, lh := range h.registry.LifecycleHandlers() {
		if err := lh.AfterAgentRun(ctx, args); err != nil {
			return err
		}
	}
	return nil
}

// InterceptInput calls all InputInterceptors until one returns a non-nil result.
func (h *RegistryHooks) InterceptInput(ctx context.Context, args agent.InterceptInputArgs) (*agent.InterceptResult, error) {
	if h.registry == nil {
		return nil, nil
	}
	for _, ii := range h.registry.InputInterceptors() {
		result, err := ii.InterceptInput(ctx, args)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
	}
	return nil, nil
}

// OnDiscoveredToolsCleared calls all LifecycleHandlers.
func (h *RegistryHooks) OnDiscoveredToolsCleared(ctx context.Context, tapeName string) error {
	if h.registry == nil {
		return nil
	}
	for _, lh := range h.registry.LifecycleHandlers() {
		if err := lh.OnDiscoveredToolsCleared(ctx, tapeName); err != nil {
			return err
		}
	}
	return nil
}

// OnContextReset calls all ContextResetHandlers.
func (h *RegistryHooks) OnContextReset(ctx context.Context, tapeName string, reason string) error {
	if h.registry == nil {
		return nil
	}
	for _, cr := range h.registry.ContextResetHandlers() {
		if err := cr.OnContextReset(ctx, tapeName, reason); err != nil {
			return err
		}
	}
	return nil
}

// BeforeToolCall checks all PolicyCheckers.
func (h *RegistryHooks) BeforeToolCall(ctx context.Context, t *tool.Tool, args map[string]any, toolCtx *tool.ToolContext) error {
	if h.registry == nil {
		return nil
	}
	for _, pc := range h.registry.PolicyCheckers() {
		if err := pc.BeforeToolCall(ctx, t, args, toolCtx); err != nil {
			return err
		}
	}
	return nil
}

// BatchBeforeToolCall checks all PolicyCheckers.
func (h *RegistryHooks) BatchBeforeToolCall(ctx context.Context, items []tool.BatchCheckItem) map[int]error {
	if h.registry == nil {
		return nil
	}
	for _, pc := range h.registry.PolicyCheckers() {
		denied, err := pc.BatchBeforeToolCall(ctx, items)
		if err != nil {
			result := make(map[int]error)
			for i := range items {
				result[i] = err
			}
			return result
		}
		if len(denied) > 0 {
			return denied
		}
	}
	return nil
}

func (h *RegistryHooks) AfterToolRound(ctx context.Context, args agent.AfterToolRoundArgs) error {
	return nil
}
