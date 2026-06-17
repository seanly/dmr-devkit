package skill

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
)

// ReviewAdapter delegates critic runs to agent skills via RunSubagent.
type ReviewAdapter struct {
	Manager *Manager
	Agent   agent.RuntimeAgent
}

// RunCritic implements agent.ReviewDelegate.
func (r *ReviewAdapter) RunCritic(ctx context.Context, tapeName, skillName, task string) (string, bool, error) {
	if r == nil || r.Manager == nil || r.Agent == nil {
		return "", false, nil
	}
	tc := &tool.ToolContext{
		Ctx:   ctx,
		Tape:  tapeName,
		State: map[string]any{tool.StateKeyRuntimeAgent: r.Agent},
	}
	res, err := r.Manager.runSkillDelegation(tc, strings.ToLower(strings.TrimSpace(skillName)), task)
	if err != nil {
		return "", false, err
	}
	m, _ := res.(map[string]any)
	if m == nil {
		return "", false, nil
	}
	if ok, _ := m["success"].(bool); !ok {
		msg, _ := m["error"].(string)
		if msg == "" {
			msg = "delegation failed"
		}
		return msg, false, nil
	}
	out, _ := m["output"].(string)
	return out, strings.Contains(strings.ToUpper(out), "[CRITICAL]"), nil
}

// RunCritic is a convenience wrapper when you already hold a RuntimeAgent.
func (m *Manager) RunCritic(ctx context.Context, ag agent.RuntimeAgent, tapeName, skillName, task string) (string, bool, error) {
	return (&ReviewAdapter{Manager: m, Agent: ag}).RunCritic(ctx, tapeName, skillName, task)
}

// MarshalReviewContext builds context JSON for independent judge runs.
func MarshalReviewContext(extra map[string]any) string {
	if extra == nil {
		extra = map[string]any{}
	}
	b, _ := json.Marshal(extra)
	return string(b)
}
