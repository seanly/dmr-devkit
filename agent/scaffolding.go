package agent

import "strings"

func (a *Agent) scaffoldingProfile() string {
	p := strings.ToLower(strings.TrimSpace(a.config.AgentPolicy.Scaffolding.Profile))
	switch p {
	case "legacy", "minimal":
		return p
	default:
		return "standard"
	}
}

func (a *Agent) scaffoldingMinimal() bool {
	return a.scaffoldingProfile() == "minimal"
}

func (a *Agent) scaffoldingLegacy() bool {
	return a.scaffoldingProfile() == "legacy"
}

func (a *Agent) clearToolsOnCompact() bool {
	// The default now preserves discovered tools; explicit clear_on_compact=true
	// restores the legacy full-clear behavior.
	policy := a.toolPersistencePolicy()
	return policy.ClearOnCompact != nil && *policy.ClearOnCompact
}

func (a *Agent) preemptiveCompactEnabled() bool {
	return !a.scaffoldingMinimal()
}

func (a *Agent) llmCompactEnabled() bool {
	return !a.scaffoldingMinimal()
}

func (a *Agent) summaryJudgeEnabled() bool {
	if a.scaffoldingMinimal() || a.scaffoldingLegacy() {
		return false
	}
	return true
}
