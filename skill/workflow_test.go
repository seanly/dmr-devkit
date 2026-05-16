package skill

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorkflow(t *testing.T) {
	content := `
# Deploy Pipeline

### 1. build
prompt: |
  Build {{.image}}
model: gpt-4o-mini

### 2. test
prompt: |
  Test {{.image}}
depends_on: [build]
`
	wf, err := ParseWorkflow(content)
	require.NoError(t, err)
	require.Len(t, wf.Steps, 2)
	assert.Equal(t, "build", wf.Steps[0].Name)
	assert.Equal(t, "gpt-4o-mini", wf.Steps[0].Model)
	assert.Contains(t, wf.Steps[0].Prompt, "Build")
	assert.Equal(t, "test", wf.Steps[1].Name)
	assert.Equal(t, []string{"build"}, wf.Steps[1].DependsOn)
}

func TestParseWorkflow_MissingSteps(t *testing.T) {
	_, err := ParseWorkflow("no steps here")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one step heading")
}

func TestParseWorkflow_NoNumbering(t *testing.T) {
	content := `
### build
prompt: "do build"

### test
prompt: "do test"
depends_on: [build]
`
	wf, err := ParseWorkflow(content)
	require.NoError(t, err)
	assert.Equal(t, "build", wf.Steps[0].Name)
	assert.Equal(t, "test", wf.Steps[1].Name)
}

func TestTopologicalOrder_Simple(t *testing.T) {
	wf := &Workflow{
		Steps: []WorkflowStep{
			{Name: "deploy", DependsOn: []string{"test"}},
			{Name: "build"},
			{Name: "test", DependsOn: []string{"build"}},
		},
	}
	ordered, err := wf.TopologicalOrder()
	require.NoError(t, err)
	names := make([]string, len(ordered))
	for i, s := range ordered {
		names[i] = s.Name
	}
	assert.Equal(t, []string{"build", "test", "deploy"}, names)
}

func TestTopologicalOrder_UnknownDependency(t *testing.T) {
	wf := &Workflow{
		Steps: []WorkflowStep{
			{Name: "a", DependsOn: []string{"missing"}},
		},
	}
	_, err := wf.TopologicalOrder()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown step")
}

func TestTopologicalOrder_Cycle(t *testing.T) {
	wf := &Workflow{
		Steps: []WorkflowStep{
			{Name: "a", DependsOn: []string{"c"}},
			{Name: "b", DependsOn: []string{"a"}},
			{Name: "c", DependsOn: []string{"b"}},
		},
	}
	_, err := wf.TopologicalOrder()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestRenderPrompt(t *testing.T) {
	tmpl := "Build {{.image}}:{{.tag}} using output: {{.build}}"
	vars := map[string]string{"image": "myapp", "tag": "v1"}
	results := map[string]string{"build": "success"}
	out, err := renderPrompt(tmpl, vars, results)
	require.NoError(t, err)
	assert.Equal(t, "Build myapp:v1 using output: success", out)
}

func TestParseWorkflow_MaxSteps(t *testing.T) {
	var lines []string
	for i := 0; i < 105; i++ {
		lines = append(lines, fmt.Sprintf("### step%d\nprompt: do thing %d", i, i))
	}
	_, err := ParseWorkflow(strings.Join(lines, "\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestParseWorkflow_MaxSize(t *testing.T) {
	// Create a workflow larger than 1MB
	content := strings.Repeat("a", 1<<20+1)
	_, err := ParseWorkflow(content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum size")
}

func TestRenderPrompt_InvalidTemplate(t *testing.T) {
	_, err := renderPrompt("{{.bad", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse prompt template")
}
