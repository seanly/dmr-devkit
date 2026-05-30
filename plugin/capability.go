package plugin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
)

// Capability identifies a feature area that a plugin can provide.
type Capability string

// Known capability constants. Plugins may also define custom capabilities.
const (
	CapTools           Capability = "tools"
	CapSystemPrompt    Capability = "system-prompt"
	CapPolicy          Capability = "policy"       // BeforeToolCall / BatchBeforeToolCall
	CapApprover        Capability = "approver"
	CapChat            Capability = "chat"
	CapInterceptor     Capability = "interceptor"  // InterceptInput
	CapLifecycle       Capability = "lifecycle"    // AfterAgentRun / DiscoveredToolsCleared
	CapHTTP            Capability = "http"         // HTTP endpoint provider
)

// CapabilitySet returns all known capability constants as a slice.
func CapabilitySet() []Capability {
	return []Capability{
		CapTools,
		CapSystemPrompt,
		CapPolicy,
		CapApprover,
		CapChat,
		CapInterceptor,
		CapLifecycle,
		CapHTTP,
	}
}

// ---------------------------------------------------------------------------
// Capability interfaces
// ---------------------------------------------------------------------------

// ToolProvider is implemented by plugins that contribute tools.
type ToolProvider interface {
	// ListTools returns the tools provided by this plugin.
	ListTools(ctx context.Context) ([]*tool.Tool, error)
}

// SystemPromptProvider is implemented by plugins that contribute fragments
// to the system prompt.
type SystemPromptProvider interface {
	// SystemPrompt returns a prompt fragment for the given base prompt.
	// Empty string means no contribution.
	SystemPrompt(ctx context.Context, base string) (string, error)
}

// PolicyChecker is implemented by plugins that enforce policy before tool calls.
type PolicyChecker interface {
	// BeforeToolCall is called before each tool execution.
	// Return a non-nil error to block the tool call.
	BeforeToolCall(ctx context.Context, t *tool.Tool, args map[string]any, toolCtx *tool.ToolContext) error

	// BatchBeforeToolCall is called for batch policy checks on multiple tool calls.
	// Return a map of denied indices (nil means all approved).
	BatchBeforeToolCall(ctx context.Context, items []tool.BatchCheckItem) (map[int]error, error)
}

// Approver is implemented by plugins that provide human-in-the-loop approval.
type Approver interface {
	// RequestApproval requests a single approval decision.
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)

	// RequestBatchApproval requests a batch approval decision.
	RequestBatchApproval(ctx context.Context, reqs []ApprovalRequest) (BatchApprovalResult, error)
}

// TapeAwareApprover extends Approver with tape-based routing.
type TapeAwareApprover interface {
	Approver
	CanHandleTape(tape string) bool
}

// ChatProvider is implemented by plugins that provide an interactive chat interface.
type ChatProvider interface {
	// CreateSession creates a new chat session for the given tape.
	CreateSession(agent agent.RuntimeAgent, tape string) ChatInterface
}

// InputInterceptor is implemented by plugins that intercept user input.
type InputInterceptor interface {
	// InterceptInput intercepts user input before the agent processes it.
	// Return a non-nil InterceptResult to short-circuit the agent loop.
	InterceptInput(ctx context.Context, args agent.InterceptInputArgs) (*agent.InterceptResult, error)
}

// LifecycleHandler is implemented by plugins that hook into agent lifecycle events.
type LifecycleHandler interface {
	// AfterAgentRun is called after an agent run completes.
	AfterAgentRun(ctx context.Context, args agent.AfterAgentRunArgs) error

	// OnDiscoveredToolsCleared is called when discovered tools are cleared for a tape.
	OnDiscoveredToolsCleared(ctx context.Context, tapeName string) error
}

// HTTPProvider is implemented by plugins that expose HTTP endpoints.
// The webserver plugin acts as a gateway and mounts these routes under
// /api/plugin/{plugin_name}/.
type HTTPProvider interface {
	// RegisterRoutes registers the plugin's HTTP handlers on the given mux.
	// basePath is the prefix assigned to this plugin, e.g. "/api/plugin/kanban".
	// The plugin should register handlers relative to basePath.
	RegisterRoutes(mux *http.ServeMux, basePath string)
}

// ---------------------------------------------------------------------------
// Capability inference and validation
// ---------------------------------------------------------------------------

// InferCapabilities returns the set of capabilities that p actually implements
// based on interface assertions. This is used by Registry to validate that a
// plugin's declared Capabilities() match its concrete implementations.
func InferCapabilities(p Plugin) []Capability {
	var caps []Capability
	if _, ok := p.(ToolProvider); ok {
		caps = append(caps, CapTools)
	}
	if _, ok := p.(SystemPromptProvider); ok {
		caps = append(caps, CapSystemPrompt)
	}
	if _, ok := p.(PolicyChecker); ok {
		caps = append(caps, CapPolicy)
	}
	if _, ok := p.(Approver); ok {
		caps = append(caps, CapApprover)
	}
	if _, ok := p.(ChatProvider); ok {
		caps = append(caps, CapChat)
	}
	if _, ok := p.(InputInterceptor); ok {
		caps = append(caps, CapInterceptor)
	}
	if _, ok := p.(LifecycleHandler); ok {
		caps = append(caps, CapLifecycle)
	}
	if _, ok := p.(HTTPProvider); ok {
		caps = append(caps, CapHTTP)
	}
	return caps
}

// ValidateCapabilities checks that the capabilities declared by p via
// Capabilities() exactly match the interfaces that p implements.
// It returns an error describing the first mismatch found, or nil if consistent.
func ValidateCapabilities(p Plugin) error {
	declared := make(map[Capability]struct{})
	for _, c := range p.Capabilities() {
		declared[c] = struct{}{}
	}

	inferred := make(map[Capability]struct{})
	for _, c := range InferCapabilities(p) {
		inferred[c] = struct{}{}
	}

	for c := range declared {
		if _, ok := inferred[c]; !ok {
			return fmt.Errorf("plugin %q declares capability %q but does not implement the corresponding interface", p.Name(), c)
		}
	}
	for c := range inferred {
		if _, ok := declared[c]; !ok {
			return fmt.Errorf("plugin %q implements capability %q interface but does not declare it in Capabilities()", p.Name(), c)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dependency injection interfaces
// ---------------------------------------------------------------------------

// AgentRunnerSetter is implemented by plugins that need the AgentRunner injected.
type AgentRunnerSetter interface {
	SetAgentRunner(runner any) error
}

// RuntimeAgentSetter is implemented by plugins that need the RuntimeAgent injected.
type RuntimeAgentSetter interface {
	SetRuntimeAgent(ra agent.RuntimeAgent) error
}

// TapeReaderSetter is implemented by plugins that need a TapeReader injected.
// The reader argument is typed as any to avoid depending on the tape package.
type TapeReaderSetter interface {
	SetTapeReader(reader any) error
}

// TapeControlSetter is implemented by plugins that need TapeControl injected.
// The ctrl argument is typed as any to avoid depending on the tape package.
type TapeControlSetter interface {
	SetTapeControl(ctrl any) error
}

// CapabilityRegistrySetter is implemented by plugins that need access to the
// full capability registry (e.g. the webserver plugin to discover HTTPProviders).
type CapabilityRegistrySetter interface {
	SetCapabilityRegistry(r *Registry) error
}
