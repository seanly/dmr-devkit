package skill

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// headingPattern matches markdown headings like "### 1. build" or "### build".
// It captures the step name (after any numbering/punctuation).
var headingPattern = regexp.MustCompile(`^#{3,}\s*(?:\d+\.\s*)?(\S+.*)$`)

// WorkflowStep represents a single step in a workflow skill.
type WorkflowStep struct {
	Name            string   `yaml:"name"`
	Prompt          string   `yaml:"prompt"`
	Model           string   `yaml:"model,omitempty"`
	DependsOn       []string `yaml:"depends_on,omitempty"`
	RequireApproval bool     `yaml:"require_approval,omitempty"`
}

// Workflow represents a declarative workflow parsed from a SKILL.md body.
type Workflow struct {
	Steps []WorkflowStep
}

// ParseWorkflow parses the markdown body after frontmatter into a Workflow.
// It looks for markdown headings (###) to identify steps, then treats the
// content under each heading as a YAML snippet for that step.
func ParseWorkflow(content string) (*Workflow, error) {
	const maxSteps = 100
	const maxContentBytes = 1 << 20 // 1 MB

	if len(content) > maxContentBytes {
		return nil, fmt.Errorf("workflow content exceeds maximum size of %d bytes", maxContentBytes)
	}

	lines := strings.Split(content, "\n")
	var steps []WorkflowStep
	var currentBlock []string
	var currentName string

	flush := func() error {
		if currentName == "" || len(currentBlock) == 0 {
			return nil
		}
		var step WorkflowStep
		if err := yaml.Unmarshal([]byte(strings.Join(currentBlock, "\n")), &step); err != nil {
			return fmt.Errorf("workflow step %q has invalid YAML: %w", currentName, err)
		}
		if step.Name == "" {
			step.Name = currentName
		}
		steps = append(steps, step)
		currentBlock = nil
		currentName = ""
		return nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := headingPattern.FindStringSubmatch(trimmed); m != nil {
			if err := flush(); err != nil {
				return nil, err
			}
			if len(steps) >= maxSteps {
				return nil, fmt.Errorf("workflow exceeds maximum of %d steps", maxSteps)
			}
			currentName = strings.TrimSpace(m[1])
			continue
		}
		if currentName != "" {
			currentBlock = append(currentBlock, line)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("workflow skill must contain at least one step heading (e.g. ### build)")
	}
	return &Workflow{Steps: steps}, nil
}

// Validate checks the workflow for structural and semantic errors.
func (w *Workflow) Validate() error {
	names := make(map[string]bool)
	for _, s := range w.Steps {
		if names[s.Name] {
			return fmt.Errorf("workflow step name %q is duplicated", s.Name)
		}
		names[s.Name] = true
		if strings.TrimSpace(s.Prompt) == "" {
			return fmt.Errorf("workflow step %q missing prompt", s.Name)
		}
	}
	_, err := w.TopologicalOrder()
	return err
}

// TopologicalOrder returns the workflow steps in dependency order.
// It validates that all dependencies exist and that there are no cycles.
func (w *Workflow) TopologicalOrder() ([]WorkflowStep, error) {
	stepIndex := make(map[string]int)
	for i, s := range w.Steps {
		stepIndex[s.Name] = i
	}

	// Validate dependency existence.
	for _, s := range w.Steps {
		for _, dep := range s.DependsOn {
			if _, ok := stepIndex[dep]; !ok {
				return nil, fmt.Errorf("workflow step %q depends on unknown step %q", s.Name, dep)
			}
		}
	}

	inDegree := make(map[string]int)
	for _, s := range w.Steps {
		if _, ok := inDegree[s.Name]; !ok {
			inDegree[s.Name] = 0
		}
		for range s.DependsOn {
			inDegree[s.Name]++
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	var ordered []WorkflowStep
	visited := make(map[string]bool)

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true
		ordered = append(ordered, w.Steps[stepIndex[name]])

		// Reduce in-degree of steps that depend on this one.
		for _, s := range w.Steps {
			for _, dep := range s.DependsOn {
				if dep == name {
					inDegree[s.Name]--
					if inDegree[s.Name] == 0 {
						queue = append(queue, s.Name)
					}
				}
			}
		}
	}

	if len(ordered) != len(w.Steps) {
		return nil, fmt.Errorf("workflow contains a dependency cycle")
	}
	return ordered, nil
}

// renderPrompt renders a workflow step prompt using text/template.
// It injects user-provided vars and the outputs of previous steps.
func renderPrompt(tmpl string, vars map[string]string, stepResults map[string]string) (string, error) {
	t := template.New("workflow")
	t = t.Funcs(template.FuncMap{
		"json": func(v string) string { return v },
	})
	t, err := t.Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse prompt template: %w", err)
	}

	data := make(map[string]string)
	for k, v := range vars {
		data[k] = v
	}
	for k, v := range stepResults {
		data[k] = v
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render prompt template: %w", err)
	}
	return buf.String(), nil
}
