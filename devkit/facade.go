package devkit

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/workflow"
)

// QuickAgentConfig is the minimal configuration for [QuickAgent].
type QuickAgentConfig struct {
	Model   string
	APIKey  string
	APIBase string

	// Tools are registered as core tools on the agent.
	Tools []*tool.Tool

	// MaxSteps caps the agent loop. Zero uses the default (20).
	MaxSteps int
	// MaxDuplicateToolCalls limits repeated identical tool calls. Zero uses the default (2).
	MaxDuplicateToolCalls int

	Workspace         string
	SystemPrompt      string
	SystemPromptExtra string
	Verbose           int
}

// QuickAgent creates an [agent.Agent] with minimal boilerplate. It is the
// fastest way to get a runnable agent for scripts and experiments.
//
// The returned agent uses an in-memory tape store and the default system prompt.
// Callers that need persistence, hooks, or workflow orchestration should use
// [Build] instead.
func QuickAgent(ctx context.Context, cfg QuickAgentConfig) (*agent.Agent, error) {
	opts := Options{
		Model:                 cfg.Model,
		APIKey:                cfg.APIKey,
		APIBase:               cfg.APIBase,
		Tools:                 cfg.Tools,
		MaxSteps:              cfg.MaxSteps,
		MaxDuplicateToolCalls: cfg.MaxDuplicateToolCalls,
		Workspace:             cfg.Workspace,
		SystemPromptBase:      cfg.SystemPrompt,
		SystemPromptExtra:     cfg.SystemPromptExtra,
		Verbose:               cfg.Verbose,
	}
	kit, err := Build(ctx, opts)
	if err != nil {
		return nil, err
	}
	return kit.Agent, nil
}

// QuickRun executes a single prompt against an agent and returns the text output.
func QuickRun(ctx context.Context, a *agent.Agent, prompt string) (string, error) {
	res, err := a.Run(ctx, DefaultTapeName, prompt, 0)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", fmt.Errorf("devkit: agent returned nil result")
	}
	return res.Output, nil
}

// QuickCrewConfig configures a multi-agent crew for [QuickCrew].
type QuickCrewConfig struct {
	// Agents are the agents in the crew, executed sequentially in the given order.
	Agents []QuickAgentConfig
	// TapePrefix is prepended to the default tape name for each agent.
	// Empty means each agent uses its own tape "crew/{index}".
	TapePrefix string
}

// QuickCrew runs a multi-agent crew sequentially. The output of each agent
// becomes the prompt for the next agent. The final agent's result is returned.
func QuickCrew(ctx context.Context, cfg QuickCrewConfig, task string) (*agent.Result, error) {
	if len(cfg.Agents) == 0 {
		return nil, fmt.Errorf("devkit: QuickCrew requires at least one agent")
	}

	kits := make([]*Kit, 0, len(cfg.Agents))
	nodes := make([]workflow.Node, 0, len(cfg.Agents))
	for i, ac := range cfg.Agents {
		opts := Options{
			Model:                 ac.Model,
			APIKey:                ac.APIKey,
			APIBase:               ac.APIBase,
			Tools:                 ac.Tools,
			MaxSteps:              ac.MaxSteps,
			MaxDuplicateToolCalls: ac.MaxDuplicateToolCalls,
			Workspace:             ac.Workspace,
			SystemPromptBase:      ac.SystemPrompt,
			SystemPromptExtra:     ac.SystemPromptExtra,
			Verbose:               ac.Verbose,
		}
		kit, err := Build(ctx, opts)
		if err != nil {
			for _, k := range kits {
				_ = k.Close(ctx)
			}
			return nil, fmt.Errorf("devkit: build crew agent %d: %w", i, err)
		}
		kits = append(kits, kit)

		tapeName := fmt.Sprintf("crew/%d", i)
		if cfg.TapePrefix != "" {
			tapeName = cfg.TapePrefix + "/" + fmt.Sprintf("%d", i)
		}
		nodes = append(nodes, kit.AsAgentNodeWithTape(fmt.Sprintf("agent-%d", i), tapeName))
	}

	seq := &workflow.Sequential{WorkflowName: "quick-crew", Nodes: nodes}
	wctx := workflow.NewContext()
	wctx.Metadata["run_id"] = newWorkflowRunID()
	res, err := seq.Run(ctx, wctx, task)
	if err != nil {
		for _, k := range kits {
			_ = k.Close(ctx)
		}
		return nil, err
	}

	for _, k := range kits {
		_ = k.Close(ctx)
	}

	wres, ok := res.(*workflow.Result)
	if !ok {
		return nil, fmt.Errorf("devkit: crew runner returned %T, expected *workflow.Result", res)
	}
	return &agent.Result{Output: fmt.Sprintf("%v", wres.Output), Steps: wres.Steps}, nil
}
