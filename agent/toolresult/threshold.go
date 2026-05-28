package toolresult

import (
	"github.com/seanly/dmr-devkit/tool"
)

// EffectivePersistThreshold computes the externalize threshold in runes.
// configured is the resolved model/agent/auto cap (positive), or -1 to disable persistence.
func EffectivePersistThreshold(t *tool.Tool, configured int, pol Policy, toolName string) int {
	if configured < 0 {
		return -1
	}
	if pol.skips(toolName) {
		return -1
	}
	if t != nil && t.Spec.MaxResultChars < 0 {
		return -1
	}
	cap := pol.effectiveMaxChars()
	base := configured
	if t != nil && t.Spec.MaxResultChars > 0 {
		base = t.Spec.MaxResultChars
	}
	if base <= 0 {
		base = cap
	}
	if base > cap {
		return cap
	}
	return base
}
