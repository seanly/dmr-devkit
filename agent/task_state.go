package agent

import "github.com/seanly/dmr-devkit/config"

// snapshotTaskStateBeforeHandoff is a no-op stub; task_state was removed.
func (a *Agent) snapshotTaskStateBeforeHandoff(tapeName string, step int) int {
	_ = tapeName
	_ = step
	return 0
}

func (a *Agent) compactCfg() config.HandoffConfig {
	cfg := a.config.AgentPolicy.Handoff
	if cfg == (config.HandoffConfig{}) {
		return config.DefaultHandoffConfig()
	}
	if cfg.MaxArtifacts <= 0 {
		cfg.MaxArtifacts = 20
	}
	if cfg.MaxActiveFiles <= 0 {
		cfg.MaxActiveFiles = 10
	}
	return cfg
}

