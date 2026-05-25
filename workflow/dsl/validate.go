package dsl

import (
	"fmt"
	"strings"
)

// Validate checks a WorkflowDef for structural errors.
// It returns a list of error strings so that callers can present all problems
// at once rather than one at a time.
func (w *WorkflowDef) Validate() []string {
	var errs []string

	if strings.TrimSpace(w.Name) == "" {
		errs = append(errs, "workflow.name is required")
	}
	if strings.TrimSpace(w.Description) == "" {
		errs = append(errs, "workflow.description is required")
	}
	if len(w.Stages) == 0 {
		errs = append(errs, "workflow.stages must contain at least one stage")
	}

	// Track ids for uniqueness checks.
	stageIDs := make(map[string]int) // id -> index
	for i, s := range w.Stages {
		if strings.TrimSpace(s.ID) == "" {
			errs = append(errs, fmt.Sprintf("stages[%d].id is required", i))
			continue
		}
		if prev, exists := stageIDs[s.ID]; exists {
			errs = append(errs, fmt.Sprintf("duplicate stage id %q at stages[%d] and stages[%d]", s.ID, prev, i))
		}
		stageIDs[s.ID] = i
	}

	// Agent id uniqueness within a stage, and non-empty checks.
	for i, s := range w.Stages {
		if len(s.Agents) == 0 {
			errs = append(errs, fmt.Sprintf("stages[%d] (%s) must contain at least one agent", i, s.ID))
		}
		agentIDs := make(map[string]int)
		for j, a := range s.Agents {
			if strings.TrimSpace(a.ID) == "" {
				errs = append(errs, fmt.Sprintf("stages[%d].agents[%d].id is required", i, j))
				continue
			}
			if prev, exists := agentIDs[a.ID]; exists {
				errs = append(errs, fmt.Sprintf("duplicate agent id %q in stage %q at agents[%d] and agents[%d]", a.ID, s.ID, prev, j))
			}
			agentIDs[a.ID] = j
			if strings.TrimSpace(a.Prompt) == "" {
				errs = append(errs, fmt.Sprintf("stages[%d].agents[%d] (%s) prompt is required", i, j, a.ID))
			}
		}
	}

	// Validate depends_on references and detect cycles.
	if len(w.Stages) > 0 {
		errs = append(errs, w.validateDependencies(stageIDs)...)
	}

	return errs
}

func (w *WorkflowDef) validateDependencies(stageIDs map[string]int) []string {
	var errs []string
	depGraph := make(map[string][]string, len(w.Stages))
	for _, s := range w.Stages {
		for _, dep := range s.DependsOn {
			if _, ok := stageIDs[dep]; !ok {
				errs = append(errs, fmt.Sprintf("stage %q depends_on references unknown stage %q", s.ID, dep))
			}
			depGraph[s.ID] = append(depGraph[s.ID], dep)
		}
	}

	// Detect cycles with DFS.
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	var dfs func(string) bool
	dfs = func(id string) bool {
		visited[id] = true
		recStack[id] = true
		for _, dep := range depGraph[id] {
			if !visited[dep] {
				if dfs(dep) {
					return true
				}
			} else if recStack[dep] {
				return true
			}
		}
		recStack[id] = false
		return false
	}
	for _, s := range w.Stages {
		if !visited[s.ID] {
			if dfs(s.ID) {
				errs = append(errs, fmt.Sprintf("dependency cycle detected involving stage %q", s.ID))
				break
			}
		}
	}
	return errs
}
