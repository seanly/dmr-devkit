package agent

import (
	"context"

	"github.com/seanly/dmr-devkit/tool"
)

// collectTools gathers tools from all registered plugins.
// Deprecated: Use collectToolsWithDiscovery instead.
func (a *Agent) collectTools(ctx context.Context) []*tool.Tool {
	return a.collectToolsWithDiscovery(ctx, "")
}

// collectToolsWithDiscoveryCached returns cached tools per tape, invalidating when discovery changes.
func (a *Agent) collectToolsWithDiscoveryCached(ctx context.Context, tapeName string) []*tool.Tool {
	if tapeName == "" {
		return a.collectToolsWithDiscovery(ctx, tapeName)
	}
	a.toolsCacheMu.RLock()
	cached, ok := a.toolsCache[tapeName]
	a.toolsCacheMu.RUnlock()
	if ok {
		return cached
	}
	tools := a.collectToolsWithDiscovery(ctx, tapeName)
	a.toolsCacheMu.Lock()
	defer a.toolsCacheMu.Unlock()
	if len(a.toolsCache) >= maxToolsCache {
		for k := range a.toolsCache {
			delete(a.toolsCache, k)
			break
		}
	}
	a.toolsCache[tapeName] = tools
	return tools
}

// collectToolsWithDiscovery gathers tools with support for deferred tool discovery.
// Core tools are always loaded. Extended tools are loaded only if discovered for the tape.
func (a *Agent) collectToolsWithDiscovery(ctx context.Context, tapeName string) []*tool.Tool {
	var tools []*tool.Tool

	// Step 1: Collect core tools (always loaded)
	tools = append(tools, a.hooks.CollectAllTools(ctx, true, false)...)

	// Step 2: Add config.Tools (user-defined tools, treated as core)
	tools = append(tools, a.config.Tools...)

	// Step 3: Collect discovered extended tools
	if tapeName != "" {
		extendedTools := a.GetAllExtendedTools()
		for _, t := range extendedTools {
			if a.IsToolDiscovered(tapeName, t.Spec.Name) {
				tools = append(tools, t)
			}
		}
	}

	return tools
}

func filterExcludedTools(tools []*tool.Tool, mode *runMode) []*tool.Tool {
	if mode == nil || len(mode.excludeToolNames) == 0 {
		return tools
	}
	out := make([]*tool.Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		if _, skip := mode.excludeToolNames[t.Spec.Name]; skip {
			continue
		}
		out = append(out, t)
	}
	return out
}

func filterAllowedTools(tools []*tool.Tool, mode *runMode) []*tool.Tool {
	if mode == nil || !mode.toolWhitelist {
		return tools
	}
	out := make([]*tool.Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		if _, ok := mode.allowedToolNames[t.Spec.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}
