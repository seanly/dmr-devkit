package agent

import (
	"github.com/seanly/dmr-devkit/agent/toolresult"
	"github.com/seanly/dmr-devkit/config"
)

// buildToolResultPolicy maps [config.AgentConfig.ToolResultPolicy] into a runtime policy.
func buildToolResultPolicy(cfg Config) toolresult.Policy {
	c := cfg.AgentPolicy.ToolResultPolicy
	skip := map[string]struct{}{"fsRead": {}}
	for _, n := range c.SkipTools {
		if n != "" {
			skip[n] = struct{}{}
		}
	}
	mcTools := map[string]struct{}{}
	for _, n := range c.Microcompact.CompactableTools {
		if n != "" {
			mcTools[n] = struct{}{}
		}
	}

	mcEnabled := false
	keepRecent := 0

	if toolResultPolicyUnset(c) {
		// Sensible CLI defaults when [agent.tool_result_policy] is omitted.
		mcEnabled = true
		keepRecent = 3
		mcTools = toolresult.DefaultMicrocompactTools()
	} else {
		// User explicitly configured the section; respect the settings.
		if c.Microcompact.Enabled != nil {
			mcEnabled = *c.Microcompact.Enabled
		}
		keepRecent = c.Microcompact.KeepRecent
	}

	if keepRecent == 0 && mcEnabled {
		keepRecent = 3
	}
	if mcEnabled && len(mcTools) == 0 {
		mcTools = toolresult.DefaultMicrocompactTools()
	}
	return toolresult.Policy{
		Workspace:        cfg.Workspace,
		DefaultMaxChars:  c.DefaultMaxChars,
		PerMessageBudget: c.PerMessageBudget,
		PreviewRunes:     c.PreviewChars,
		PersistSubdir:    c.PersistSubdir,
		SkipTools:        skip,
		Microcompact: toolresult.MicrocompactPolicy{
			Enabled:          mcEnabled,
			KeepRecent:       keepRecent,
			CompactableTools: mcTools,
			GapMinutes:       c.Microcompact.GapMinutes,
		},
	}
}

func toolResultPolicyUnset(c config.ToolResultPolicyConfig) bool {
	return c.DefaultMaxChars == 0 && c.PerMessageBudget == 0 && c.PreviewChars == 0 &&
		c.PersistSubdir == "" && len(c.SkipTools) == 0 &&
		c.Microcompact.Enabled == nil && c.Microcompact.KeepRecent == 0 &&
		len(c.Microcompact.CompactableTools) == 0 && c.Microcompact.GapMinutes == 0
}
