package tool

import (
	"context"

	"github.com/seanly/dmr-devkit/cwd"
)

// CwdPolicy defines how CWD (current working directory) should be handled.
type CwdPolicy int

const (
	// CwdPolicyAllow allows commands to change CWD freely.
	CwdPolicyAllow CwdPolicy = iota
	// CwdPolicyTrack allows CWD changes but tracks them.
	CwdPolicyTrack
	// CwdPolicyPrevent prevents commands from changing CWD (sandbox mode).
	CwdPolicyPrevent
)

// StateKey constants for well-known ToolContext.State keys.
// New code should prefer typed fields (e.g. ToolContext.Workspace) over State.
const (
	StateKeyRuntimeWorkspace = "_runtime_workspace"
	StateKeyTapeStore        = "_tape_store"
	StateKeyTapeManager      = "_tape_manager"
	StateKeyRuntimeAgent     = "_runtime_agent"
)

// ToolContext provides contextual information to tool handlers.
type ToolContext struct {
	// Ctx carries the Go context.Context for cancellation and deadline propagation.
	Ctx   context.Context
	Tape  string
	RunID string
	Meta  map[string]any
	State map[string]any
	// Context is an optional map for plugin-to-tool context passing.
	// When a plugin calls RunAgent with ContextJSON, it is unmarshaled into this field
	// and made available to all tool calls during that agent session.
	Context map[string]any

	// Workspace is the agent's configured workspace directory.
	// It replaces the legacy State["_runtime_workspace"] injection.
	Workspace string

	// CwdManager tracks and manages the current working directory.
	// If nil, tools should fall back to Workspace.
	CwdManager *cwd.Manager

	// CwdPolicy controls how CWD changes are handled.
	CwdPolicy CwdPolicy

	// CwdMustBeUnder is a base directory that CWD must not escape.
	// If empty, no restriction is applied.
	// Used to ensure all file operations stay within workspace.
	CwdMustBeUnder string
}

// NewToolContext creates a new ToolContext with initialized maps.
func NewToolContext(ctx context.Context, tape, runID string) *ToolContext {
	return &ToolContext{
		Ctx:       ctx,
		Tape:      tape,
		RunID:     runID,
		Meta:      make(map[string]any),
		State:     make(map[string]any),
		Context:   make(map[string]any),
		CwdPolicy: CwdPolicyTrack, // Default: track CWD changes
	}
}

// GetCwd returns the current working directory.
// Priority: CwdManager > Workspace field > legacy State["_runtime_workspace"].
func (ctx *ToolContext) GetCwd() string {
	if ctx.CwdManager != nil {
		return ctx.CwdManager.Get()
	}
	if ctx.Workspace != "" {
		return ctx.Workspace
	}
	// Backward compatibility: fall back to legacy State key.
	if ws, ok := ctx.State[StateKeyRuntimeWorkspace].(string); ok {
		return ws
	}
	return ""
}

// CheckCwdAllowed checks if a target directory is allowed under CwdMustBeUnder.
// Returns nil if allowed, error otherwise.
func (ctx *ToolContext) CheckCwdAllowed(targetCwd string) error {
	if ctx.CwdMustBeUnder == "" {
		return nil // No restriction
	}

	// If no CwdManager, we can't check
	if ctx.CwdManager == nil {
		return nil
	}

	return ctx.CwdManager.MustBeUnder(targetCwd, ctx.CwdMustBeUnder)
}

// RecoverCwd attempts to recover CWD if it was deleted.
// Returns the (possibly new) CWD and whether recovery was performed.
func (ctx *ToolContext) RecoverCwd() (string, bool, error) {
	if ctx.CwdManager == nil {
		return ctx.GetCwd(), false, nil
	}
	return ctx.CwdManager.RecoverIfDeleted()
}
