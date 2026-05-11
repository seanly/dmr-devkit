package agent

import (
	"context"

	"github.com/seanly/dmr-devkit/tool"
)

// Hooks is the narrow extension surface for the agent loop. Implementations live
// in github.com/seanly/dmr/pkg/plugin (e.g. *Manager) for full DMR CLI; embedders pass nil for a no-op hooks.
type Hooks interface {
	ComposeSystemPrompt(ctx context.Context, base string) string
	CollectAllTools(ctx context.Context, includeCore, includeExtended bool) []*tool.Tool
	AfterAgentRun(ctx context.Context, args AfterAgentRunArgs) error
	InterceptInput(ctx context.Context, args InterceptInputArgs) (*InterceptResult, error)
	OnDiscoveredToolsCleared(ctx context.Context, tapeName string) error
	BeforeToolCall(ctx context.Context, t *tool.Tool, args map[string]any, toolCtx *tool.ToolContext) error
	BatchBeforeToolCall(ctx context.Context, items []tool.BatchCheckItem) map[int]error
}

type noopHooks struct{}

func (noopHooks) ComposeSystemPrompt(_ context.Context, base string) string { return base }

func (noopHooks) CollectAllTools(context.Context, bool, bool) []*tool.Tool { return nil }

func (noopHooks) AfterAgentRun(context.Context, AfterAgentRunArgs) error { return nil }

func (noopHooks) InterceptInput(context.Context, InterceptInputArgs) (*InterceptResult, error) {
	return nil, nil
}

func (noopHooks) OnDiscoveredToolsCleared(context.Context, string) error { return nil }

func (noopHooks) BeforeToolCall(context.Context, *tool.Tool, map[string]any, *tool.ToolContext) error {
	return nil
}

func (noopHooks) BatchBeforeToolCall(context.Context, []tool.BatchCheckItem) map[int]error {
	return nil
}

// NopHooks returns a [Hooks] implementation that does not extend the agent loop.
func NopHooks() Hooks { return noopHooks{} }
