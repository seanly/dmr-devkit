package a2aserver

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/agent"
)

// AgentRunner adapts [*agent.Agent] to [Runner], forwarding contextJSON into the agent loop.
type AgentRunner struct {
	Agent *agent.Agent
}

func (r *AgentRunner) Run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32, contextJSON string) (*agent.Result, error) {
	if r == nil || r.Agent == nil {
		return nil, fmt.Errorf("a2aserver: agent is nil")
	}
	return r.Agent.RunWithContext(ctx, tapeName, prompt, historyAfterEntryID, contextJSON)
}
