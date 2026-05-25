package compiler

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/workflow"
	"github.com/seanly/dmr-devkit/workflow/dsl"
)

// BindAgents replaces all placeholder nodes in the compiled graph with real
// AgentNodes backed by the given devkit Kit.
func (cw *CompiledWorkflow) BindAgents(kit *devkit.Kit) error {
	factory := func(nodeID, tapeName string, def dsl.AgentDef) workflow.Node {
		node := kit.AsAgentNodeWithTape(nodeID, tapeName)
		node.SystemPrompt = def.SystemPrompt
		if def.Tools != nil {
			s := append([]string(nil), (*def.Tools)...)
			node.AllowedTools = &s
		}
		node.Model = def.Model
		return node
	}
	return cw.BindWithFactory(factory)
}

// BindWithFactory replaces placeholder nodes using a caller-provided factory.
// This allows the dmr plugin (which does not have a devkit.Kit) to supply its
// own agent node implementation.
func (cw *CompiledWorkflow) BindWithFactory(factory func(nodeID, tapeName string, def dsl.AgentDef) workflow.Node) error {
	for _, s := range cw.Def.Stages {
		for _, a := range s.Agents {
			nodeID := fmt.Sprintf("%s:%s", s.ID, a.ID)
			_, ok := cw.Graph.Nodes[nodeID]
			if !ok {
				return fmt.Errorf("graph missing placeholder for %s", nodeID)
			}

			tapeName := fmt.Sprintf("workflow/%s/%s", cw.Def.Name, nodeID)
			baseNode := factory(nodeID, tapeName, a)

			// Wrap with prompt template rendering.
			promptT := cw.Prompts[promptKey(s.ID, a.ID)]
			cw.Graph.Nodes[nodeID] = &templatedNode{
				Node:       baseNode,
				promptT:    promptT,
				stageID:    s.ID,
				agentID:    a.ID,
				interrupt:  a.Interrupt,
				vars:       a.Variables,
			}
		}
	}
	return nil
}

// templatedNode wraps an agent node so that its prompt (and optional variables)
// are rendered from the workflow state before execution.
type templatedNode struct {
	Node      workflow.Node
	promptT   *template.Template
	stageID   string
	agentID   string
	interrupt bool
	vars      map[string]string
}

func (n *templatedNode) Name() string { return n.Node.Name() }

func (n *templatedNode) Run(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
	// Inject extra variables into state for this agent only.
	if len(n.vars) > 0 {
		for k, v := range n.vars {
			wctx.SetState(fmt.Sprintf("vars.%s", k), v)
		}
	}

	// Render prompt template.
	var buf bytes.Buffer
	if err := n.promptT.Execute(&buf, wctx.State); err != nil {
		return nil, fmt.Errorf("render prompt for %s:%s: %w", n.stageID, n.agentID, err)
	}

	// Execute the underlying agent node with the rendered prompt.
	out, err := n.Node.Run(ctx, wctx, buf.String())
	if err != nil {
		return out, err
	}

	// Store output in state so downstream agents can reference it.
	wctx.SetState(dsl.AgentOutputKey(n.stageID, n.agentID), out)

	// If interrupt is requested, pause for human approval.
	if n.interrupt {
		val, err := workflow.Interrupt(wctx, out)
		if err != nil {
			return out, err // *InterruptError — workflow pauses
		}
		return val, nil // resumed
	}

	return out, nil
}
