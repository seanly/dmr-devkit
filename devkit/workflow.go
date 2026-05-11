package devkit

import (
	"context"
	"fmt"
	"iter"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/workflow"
)

// AgentNode wraps a devkit [*Kit].Agent into a [workflow.Node] so it can be
// orchestrated by Sequential, Parallel, Graph, or custom workflows.
type AgentNode struct {
	Kit          *Kit
	AgentName    string
	TapeName     string
	SystemPrompt string
}

// Name returns the node name; satisfies [workflow.Node].
func (a *AgentNode) Name() string {
	if a.AgentName != "" {
		return a.AgentName
	}
	return a.TapeName
}

// Run executes the agent with the given input (expected string prompt).
func (a *AgentNode) Run(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
	prompt := ""
	switch v := input.(type) {
	case string:
		prompt = v
	case map[string]any:
		// If input is a map, try to find a "prompt" key, otherwise serialize.
		if p, ok := v["prompt"].(string); ok {
			prompt = p
		} else {
			prompt = fmt.Sprintf("%v", v)
		}
	default:
		prompt = fmt.Sprintf("%v", input)
	}

	// Append optional system prompt override via state injection.
	if a.SystemPrompt != "" {
		if wctx != nil {
			wctx.SetState(a.Name()+".system_prompt", a.SystemPrompt)
		}
	}

	res, err := a.Kit.Agent.Run(ctx, a.TapeName, prompt, 0)
	if err != nil {
		return nil, err
	}
	return res.Output, nil
}

// RunWorkflow executes a [workflow.Runner] (Sequential, Parallel, Graph, etc.)
// using this Kit's agent and tape infrastructure.
//
// It creates a workflow.Context with metadata pointing back to the Kit so that
// custom nodes can access tape, agent, and hooks via the Context.State/Metadata.
func (k *Kit) RunWorkflow(ctx context.Context, runner workflow.Runner, input any) (*workflow.Result, error) {
	wctx := workflow.NewContext()
	wctx.Metadata["kit"] = k
	wctx.Metadata["tape_manager"] = k.TapeManager
	wctx.Metadata["agent"] = k.Agent
	wctx.Metadata["hooks"] = k.Hooks
	out, err := runner.Run(ctx, wctx, input)
	res, ok := out.(*workflow.Result)
	if !ok {
		return nil, fmt.Errorf("devkit: workflow runner returned %T, expected *workflow.Result", out)
	}
	return res, err
}

// RunWorkflowStream executes an [workflow.EventStream] and yields execution
// events for real-time observation.
func (k *Kit) RunWorkflowStream(ctx context.Context, runner workflow.EventStream, input any) iter.Seq2[*workflow.Event, error] {
	return func(yield func(*workflow.Event, error) bool) {
		wctx := workflow.NewContext()
		wctx.Metadata["kit"] = k
		wctx.Metadata["tape_manager"] = k.TapeManager
		wctx.Metadata["agent"] = k.Agent
		wctx.Metadata["hooks"] = k.Hooks
		for ev, err := range runner.RunEvents(ctx, wctx, input) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

// AgentNodeResult wraps an [agent.Result] so it satisfies [workflow.Node].
// It is useful when you want the full Result (including Steps, Token counts)
// rather than just the string Output.
type AgentNodeResult struct {
	Res *agent.Result
}

// AsAgentNode converts this Kit into a [workflow.Node] using the default tape.
func (k *Kit) AsAgentNode(name string) *AgentNode {
	return &AgentNode{
		Kit:       k,
		AgentName: name,
		TapeName:  DefaultTapeName,
	}
}

// AsAgentNodeWithTape converts this Kit into a [workflow.Node] with a specific tape.
func (k *Kit) AsAgentNodeWithTape(name, tapeName string) *AgentNode {
	return &AgentNode{
		Kit:       k,
		AgentName: name,
		TapeName:  tapeName,
	}
}
