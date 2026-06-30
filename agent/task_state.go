package agent

import (
	"log/slog"

	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/handoff"
	"github.com/seanly/dmr-devkit/tape"
)

func (a *Agent) handoffCfg() config.HandoffConfig {
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
	if cfg.StateUpdate == "" {
		cfg.StateUpdate = "llm_extract"
	}
	return cfg
}

func (a *Agent) stateUpdater() *handoff.Updater {
	h := a.handoffCfg()
	return handoff.NewUpdater(handoff.UpdaterOptions{
		MaxArtifacts:   h.MaxArtifacts,
		MaxActiveFiles: h.MaxActiveFiles,
	})
}

func (a *Agent) taskStateEnabled() bool {
	return a.handoffCfg().StateEnabledOrDefault()
}

func (a *Agent) fetchTapeEntries(tapeName string) ([]tape.TapeEntry, error) {
	return a.tape.Store.FetchAll(tapeName, nil)
}

func (a *Agent) latestTaskState(tapeName string) *handoff.State {
	if !a.taskStateEnabled() {
		return nil
	}
	entries, err := a.fetchTapeEntries(tapeName)
	if err != nil {
		return nil
	}
	return handoff.LatestState(entries)
}

func (a *Agent) appendTaskState(tapeName string, s handoff.State) int {
	if err := s.Validate(); err != nil {
		slog.Warn("task_state validate failed", "error", err)
		return 0
	}
	entry := tape.NewTaskStateEntry(s.ToPayload())
	if err := a.tape.AppendEntry(tapeName, entry); err != nil {
		slog.Warn("task_state append failed", "tape", tapeName, "error", err)
		return 0
	}
	entries, err := a.fetchTapeEntries(tapeName)
	if err != nil {
		return 0
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "task_state" {
			return entries[i].ID
		}
	}
	return 0
}

// snapshotTaskStateBeforeHandoff writes task_state with source=handoff before compact/anchor.
func (a *Agent) snapshotTaskStateBeforeHandoff(tapeName string, step int) int {
	if !a.taskStateEnabled() {
		return 0
	}
	prev := a.latestTaskState(tapeName)
	s := a.stateUpdater().SnapshotForHandoff(prev, "handoff")
	return a.appendTaskState(tapeName, s)
}

func (a *Agent) initTaskStateFromPrompt(tapeName, prompt string) {
	if !a.taskStateEnabled() || prompt == "" {
		return
	}
	if prev := a.latestTaskState(tapeName); prev != nil {
		return
	}
	goal := prompt
	if len([]rune(goal)) > 200 {
		goal = string([]rune(goal)[:200])
	}
	_ = a.appendTaskState(tapeName, handoff.NewState(goal, "heuristic"))
}
