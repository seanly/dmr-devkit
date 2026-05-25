package compiler

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/seanly/dmr-devkit/workflow"
	"github.com/seanly/dmr-devkit/workflow/dsl"
)

// CompiledWorkflow holds a compiled graph together with the prompt and return
// templates that need late-bound rendering.
type CompiledWorkflow struct {
	Def        *dsl.WorkflowDef
	Graph      *workflow.Graph
	Prompts    map[string]*template.Template // key = "stageID:agentID"
	ReturnT    *template.Template
	SaveFiles  map[string]*template.Template
}

// BuildGraph compiles a WorkflowDef into a workflow.Graph without binding any
// agent-specific nodes.  Callers must then bind the actual agent nodes (see
// binding.go) before execution.
func BuildGraph(def *dsl.WorkflowDef) (*CompiledWorkflow, error) {
	// 1. Validate.
	if errs := def.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("validation failed: %v", errs)
	}

	// 2. Topological sort of stages.
	ordered, err := topoSort(def.Stages)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	// 3. Build graph.
	g := &workflow.Graph{Name: def.Name}

	// Pre-compile all prompt templates.
	prompts := make(map[string]*template.Template)
	for _, s := range def.Stages {
		for _, a := range s.Agents {
			key := promptKey(s.ID, a.ID)
			tmpl, err := template.New(key).Parse(a.Prompt)
			if err != nil {
				return nil, fmt.Errorf("stage %q agent %q prompt template: %w", s.ID, a.ID, err)
			}
			prompts[key] = tmpl
		}
	}

	// Pre-compile return template.
	var returnT *template.Template
	if def.Return.Template != "" {
		var err error
		returnT, err = template.New("return").Parse(def.Return.Template)
		if err != nil {
			return nil, fmt.Errorf("return template: %w", err)
		}
	}

	// Pre-compile save_files templates.
	saveFiles := make(map[string]*template.Template)
	for fname, contentTmpl := range def.Return.SaveFiles {
		tmpl, err := template.New("save:"+fname).Parse(contentTmpl)
		if err != nil {
			return nil, fmt.Errorf("save_files %q template: %w", fname, err)
		}
		saveFiles[fname] = tmpl
	}

	// Track the last node name for each stage so downstream stages can wire edges.
	stageLastNode := make(map[string]string)

	// Build nodes and edges for each stage.
	for _, stageIdx := range ordered {
		stage := def.Stages[stageIdx]
		buildStage(g, stage, stageLastNode, prompts)
	}

	// Wire END from the final stage(s).
	for _, stageIdx := range ordered {
		stage := def.Stages[stageIdx]
		if last, ok := stageLastNode[stage.ID]; ok {
			// Check if any other stage depends on this one.
			hasDependent := false
			for _, s := range def.Stages {
				for _, dep := range s.DependsOn {
					if dep == stage.ID {
						hasDependent = true
						break
					}
				}
				if hasDependent {
					break
				}
			}
			if !hasDependent {
				g.AddEdge(last, "END")
			}
		}
	}

	return &CompiledWorkflow{
		Def:       def,
		Graph:     g,
		Prompts:   prompts,
		ReturnT:   returnT,
		SaveFiles: saveFiles,
	}, nil
}

// RenderPrompt executes the pre-compiled prompt template for a given agent
// using the current workflow state.
func (cw *CompiledWorkflow) RenderPrompt(stageID, agentID string, state map[string]any) (string, error) {
	key := promptKey(stageID, agentID)
	tmpl, ok := cw.Prompts[key]
	if !ok {
		return "", fmt.Errorf("no prompt template for %s", key)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, state); err != nil {
		return "", fmt.Errorf("render prompt for %s: %w", key, err)
	}
	return buf.String(), nil
}

// RenderReturn executes the return template using the final workflow state.
func (cw *CompiledWorkflow) RenderReturn(state map[string]any) (string, error) {
	if cw.ReturnT == nil {
		return "", nil
	}
	var buf bytes.Buffer
	if err := cw.ReturnT.Execute(&buf, state); err != nil {
		return "", fmt.Errorf("render return: %w", err)
	}
	return buf.String(), nil
}

// RenderSaveFile executes a save_files template.
func (cw *CompiledWorkflow) RenderSaveFile(name string, state map[string]any) (string, error) {
	tmpl, ok := cw.SaveFiles[name]
	if !ok {
		return "", fmt.Errorf("no save file template for %q", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, state); err != nil {
		return "", fmt.Errorf("render save file %q: %w", name, err)
	}
	return buf.String(), nil
}

// buildStage adds nodes and edges for a single stage into the graph.
func buildStage(g *workflow.Graph, stage dsl.StageDef, stageLastNode map[string]string, prompts map[string]*template.Template) {
	// Determine input node for this stage.
	var stageInputNode string
	if len(stage.DependsOn) == 0 {
		stageInputNode = "START"
	} else {
		// Create a merge node that waits for all dependencies.
		mergeID := "merge:" + stage.ID
		g.AddNode(mergeID, &mergeNode{id: mergeID})
		for _, dep := range stage.DependsOn {
			if last, ok := stageLastNode[dep]; ok {
				g.AddEdge(last, mergeID)
			}
		}
		stageInputNode = mergeID
	}

	if stage.Parallel && len(stage.Agents) > 1 {
		// Fan-out: each agent is a separate branch.
		agentNodeIDs := make([]string, len(stage.Agents))
		for i, agent := range stage.Agents {
			nodeID := fmt.Sprintf("%s:%s", stage.ID, agent.ID)
			agentNodeIDs[i] = nodeID
			// Placeholder node — will be replaced by BindAgents.
			g.AddNode(nodeID, &placeholderNode{name: nodeID})
			g.AddEdge(stageInputNode, nodeID)
		}

		// Join node collects all branch outputs.
		joinID := "join:" + stage.ID
		join := &joinNode{
			id:      joinID,
			stageID: stage.ID,
			agents:  stage.Agents,
		}
		g.AddNode(joinID, join)
		for _, nid := range agentNodeIDs {
			g.AddEdge(nid, joinID)
		}
		stageLastNode[stage.ID] = joinID
	} else {
		// Sequential chain within the stage.
		lastNode := stageInputNode
		for _, agent := range stage.Agents {
			nodeID := fmt.Sprintf("%s:%s", stage.ID, agent.ID)
			g.AddNode(nodeID, &placeholderNode{name: nodeID})
			g.AddEdge(lastNode, nodeID)
			lastNode = nodeID
		}
		stageLastNode[stage.ID] = lastNode
	}
}

func promptKey(stageID, agentID string) string {
	return stageID + ":" + agentID
}

// topoSort returns stage indices in dependency order (Kahn's algorithm).
func topoSort(stages []dsl.StageDef) ([]int, error) {
	n := len(stages)
	idToIdx := make(map[string]int, n)
	for i, s := range stages {
		idToIdx[s.ID] = i
	}

	adj := make([][]int, n)
	indeg := make([]int, n)
	for i, s := range stages {
		for _, dep := range s.DependsOn {
			j, ok := idToIdx[dep]
			if !ok {
				return nil, fmt.Errorf("unknown dependency %q", dep)
			}
			adj[j] = append(adj[j], i)
			indeg[i]++
		}
	}

	var q []int
	for i, d := range indeg {
		if d == 0 {
			q = append(q, i)
		}
	}

	var result []int
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		result = append(result, u)
		for _, v := range adj[u] {
			indeg[v]--
			if indeg[v] == 0 {
				q = append(q, v)
			}
		}
	}

	if len(result) != n {
		return nil, fmt.Errorf("cycle detected")
	}
	return result, nil
}

// --- helper node types ---

// placeholderNode is a stand-in for agent nodes before binding.
// It panics if executed, reminding the caller to bind real nodes.
type placeholderNode struct {
	name string
}

func (n *placeholderNode) Name() string { return n.name }
func (n *placeholderNode) Run(context.Context, *workflow.Context, any) (any, error) {
	return nil, fmt.Errorf("placeholder node %q was not replaced with a real agent node; call BindAgents", n.name)
}

// mergeNode simply passes its input through.  Its real purpose is to act as a
// convergence point for multiple dependency edges.
type mergeNode struct {
	id string
}

func (n *mergeNode) Name() string { return n.id }
func (n *mergeNode) Run(_ context.Context, _ *workflow.Context, input any) (any, error) {
	return input, nil
}

// joinNode receives the aggregated branch outputs from a parallel fan-out and
// stores each agent result into workflow state so that downstream templates can
// reference them.
type joinNode struct {
	id      string
	stageID string
	agents  []dsl.AgentDef
}

func (n *joinNode) Name() string { return n.id }
func (n *joinNode) Run(_ context.Context, wctx *workflow.Context, input any) (any, error) {
	// When Graph.runWithJoin executes parallel branches, it passes the joined
	// result as a map with a "branches" key containing a []any slice.
	if m, ok := input.(map[string]any); ok {
		if branches, ok := m["branches"].([]any); ok {
			for i, a := range n.agents {
				if i < len(branches) {
					wctx.SetState(dsl.AgentOutputKey(n.stageID, a.ID), branches[i])
				}
			}
		}
	}
	return input, nil
}
