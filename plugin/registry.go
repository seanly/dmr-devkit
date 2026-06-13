package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Registry collects plugins and indexes them by capability.
// It is the canonical replacement for HookRegistry in the new capability model.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin // keyed by plugin name
	order   []string          // insertion order for deterministic shutdown
	byCap   map[Capability][]Plugin
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
		byCap:   make(map[Capability][]Plugin),
	}
}

// Register adds a plugin to the registry.
// Returns an error if a plugin with the same name is already registered,
// or if the plugin's declared Capabilities() do not match its implemented interfaces.
func (r *Registry) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	if existing, ok := r.plugins[name]; ok && existing != nil {
		return fmt.Errorf("plugin %q already registered", name)
	}

	if err := ValidateCapabilities(p); err != nil {
		return fmt.Errorf("plugin %q capability mismatch: %w", name, err)
	}

	r.plugins[name] = p
	r.order = append(r.order, name)
	for _, c := range p.Capabilities() {
		r.byCap[c] = append(r.byCap[c], p)
	}
	return nil
}

// Unregister removes a plugin from the registry by name.
// Returns true if the plugin was found and removed.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.plugins[name]
	if !ok {
		return false
	}

	delete(r.plugins, name)

	// Remove from order slice.
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}

	// Remove from capability index.
	for _, c := range p.Capabilities() {
		slice := r.byCap[c]
		for i, ep := range slice {
			if ep.Name() == name {
				r.byCap[c] = append(slice[:i], slice[i+1:]...)
				break
			}
		}
		if len(r.byCap[c]) == 0 {
			delete(r.byCap, c)
		}
	}

	return true
}

// Get returns a registered plugin by name, or nil if not found.
func (r *Registry) Get(name string) Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[name]
}

// List returns all registered plugins in insertion order.
func (r *Registry) List() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Plugin, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.plugins[name])
	}
	return out
}

// HasCapability returns true if any registered plugin declares the given capability.
func (r *Registry) HasCapability(cap Capability) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byCap[cap]) > 0
}

// ---------------------------------------------------------------------------
// Capability query helpers
// ---------------------------------------------------------------------------

// pluginsForCap returns the plugins indexed for a given capability.
// Caller must hold at least an RLock.
func (r *Registry) pluginsForCap(cap Capability) []Plugin {
	return r.byCap[cap]
}

// ToolProviders returns all registered plugins that implement ToolProvider.
func (r *Registry) ToolProviders() []ToolProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ToolProvider
	for _, p := range r.pluginsForCap(CapTools) {
		if pp, ok := p.(ToolProvider); ok {
			out = append(out, pp)
		}
	}
	return out
}

// SystemPromptProviders returns all registered plugins that implement SystemPromptProvider.
func (r *Registry) SystemPromptProviders() []SystemPromptProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []SystemPromptProvider
	for _, p := range r.pluginsForCap(CapSystemPrompt) {
		if pp, ok := p.(SystemPromptProvider); ok {
			out = append(out, pp)
		}
	}
	return out
}

// PolicyCheckers returns all registered plugins that implement PolicyChecker.
func (r *Registry) PolicyCheckers() []PolicyChecker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []PolicyChecker
	for _, p := range r.pluginsForCap(CapPolicy) {
		if pp, ok := p.(PolicyChecker); ok {
			out = append(out, pp)
		}
	}
	return out
}

// Approvers returns all registered plugins that implement Approver.
func (r *Registry) Approvers() []Approver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Approver
	for _, p := range r.pluginsForCap(CapApprover) {
		if pp, ok := p.(Approver); ok {
			out = append(out, pp)
		}
	}
	return out
}

// TapeAwareApprovers returns all registered plugins that implement TapeAwareApprover.
func (r *Registry) TapeAwareApprovers() []TapeAwareApprover {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []TapeAwareApprover
	for _, p := range r.pluginsForCap(CapApprover) {
		if pp, ok := p.(TapeAwareApprover); ok {
			out = append(out, pp)
		}
	}
	return out
}

// ChatProviders returns all registered plugins that implement ChatProvider.
func (r *Registry) ChatProviders() []ChatProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ChatProvider
	for _, p := range r.pluginsForCap(CapChat) {
		if pp, ok := p.(ChatProvider); ok {
			out = append(out, pp)
		}
	}
	return out
}

// InputInterceptors returns all registered plugins that implement InputInterceptor.
func (r *Registry) InputInterceptors() []InputInterceptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []InputInterceptor
	for _, p := range r.pluginsForCap(CapInterceptor) {
		if pp, ok := p.(InputInterceptor); ok {
			out = append(out, pp)
		}
	}
	return out
}

// LifecycleHandlers returns all registered plugins that implement LifecycleHandler.
func (r *Registry) LifecycleHandlers() []LifecycleHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []LifecycleHandler
	for _, p := range r.pluginsForCap(CapLifecycle) {
		if pp, ok := p.(LifecycleHandler); ok {
			out = append(out, pp)
		}
	}
	return out
}

// ContextResetHandlers returns all registered plugins that implement ContextResetHandler.
func (r *Registry) ContextResetHandlers() []ContextResetHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ContextResetHandler
	for _, p := range r.pluginsForCap(CapContextReset) {
		if pp, ok := p.(ContextResetHandler); ok {
			out = append(out, pp)
		}
	}
	return out
}

// HTTPProviders returns all registered plugins that implement HTTPProvider.
func (r *Registry) HTTPProviders() []HTTPProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []HTTPProvider
	for _, p := range r.pluginsForCap(CapHTTP) {
		if pp, ok := p.(HTTPProvider); ok {
			out = append(out, pp)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// InitAll initializes all registered plugins in insertion order.
// Each plugin is always attempted even if a previous init failed.
func (r *Registry) InitAll(ctx context.Context, configs map[string]map[string]any) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs []error
	for _, name := range r.order {
		p := r.plugins[name]
		cfg := configs[p.Name()]
		if err := p.Init(ctx, cfg); err != nil {
			errs = append(errs, fmt.Errorf("init plugin %q: %w", p.Name(), err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// ShutdownAll shuts down all plugins in reverse registration order.
// Each plugin is always attempted even if a previous shutdown failed.
func (r *Registry) ShutdownAll(ctx context.Context) error {
	r.mu.RLock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	r.mu.RUnlock()

	var errs []error
	for i := len(order) - 1; i >= 0; i-- {
		p := r.plugins[order[i]]
		if err := p.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown plugin %q: %w", p.Name(), err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// HealthCheckAll runs HealthCheck on every plugin that implements HealthChecker.
// Returns a map of plugin name -> error (nil means healthy).
func (r *Registry) HealthCheckAll(ctx context.Context) map[string]error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]error)
	for _, p := range r.plugins {
		if hc, ok := p.(HealthChecker); ok {
			results[p.Name()] = hc.HealthCheck(ctx)
		}
	}
	return results
}
