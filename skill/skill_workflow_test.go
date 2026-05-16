package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRuntimeAgent struct {
	outputs []string
	index   int
}

func (m *mockRuntimeAgent) AllModelInfos() []agent.ModelInfo                            { return nil }
func (m *mockRuntimeAgent) GetCurrentModelName(string) (string, string)                 { return "", "" }
func (m *mockRuntimeAgent) SwitchModel(string, string) error                            { return nil }
func (m *mockRuntimeAgent) CompactTape(context.Context, string) (string, error)         { return "", nil }
func (m *mockRuntimeAgent) RestartProcess() error                                       { return nil }
func (m *mockRuntimeAgent) Run(context.Context, string, string, int32) (*agent.RunResult, error) {
	return nil, nil
}
func (m *mockRuntimeAgent) SetOnToolCall(func(agent.ToolCallEvent)) {}
func (m *mockRuntimeAgent) RunSubagent(context.Context, string, string, string, string, string, int) (string, error) {
	if m.index >= len(m.outputs) {
		return "", fmt.Errorf("no more mock outputs")
	}
	out := m.outputs[m.index]
	m.index++
	return out, nil
}

func TestHandleSkillWorkflow(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "deploy")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	content := `---
name: deploy
description: Deploy pipeline
type: workflow
secrets:
  - name: REGISTRY_TOKEN
    description: Registry auth
    where: docs
---

# Deploy Workflow

### build
prompt: |
  Build {{.image}}

### test
prompt: |
  Test with {{.image}} after build={{.build}}
depends_on: [build]
`
	require.NoError(t, os.WriteFile(loc, []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	cfg.AutoCreatePath = filepath.Join(tmp, "auto")
	mgr := NewManager(cfg)

	mock := &mockRuntimeAgent{outputs: []string{"build-ok", "test-ok"}}
	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": mock},
	}

	res, err := mgr.handleSkillWorkflow(ctx, map[string]any{
		"name": "deploy",
		"vars": map[string]any{"image": "myapp"},
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.True(t, m["success"].(bool))
	results := m["results"].(map[string]string)
	assert.Equal(t, "build-ok", results["build"])
	assert.Equal(t, "test-ok", results["test"])
	assert.Equal(t, 2, mock.index)

	secrets := m["secrets"].([]map[string]string)
	require.Len(t, secrets, 1)
	assert.Equal(t, "REGISTRY_TOKEN", secrets[0]["name"])
	assert.Equal(t, "Registry auth", secrets[0]["description"])
}

func TestHandleSkillWorkflow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	cfg.AutoCreatePath = filepath.Join(tmp, "auto")
	mgr := NewManager(cfg)

	res, err := mgr.handleSkillWorkflow(nil, map[string]any{"name": "missing"})
	require.NoError(t, err)
	assert.False(t, res.(map[string]any)["success"].(bool))
}

func TestHandleSkillWorkflow_NotWorkflow(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "plain")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	require.NoError(t, os.WriteFile(loc, []byte("---\nname: plain\ndescription: plain\n---\nbody\n"), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	cfg.AutoCreatePath = filepath.Join(tmp, "auto")
	mgr := NewManager(cfg)

	res, err := mgr.handleSkillWorkflow(nil, map[string]any{"name": "plain"})
	require.NoError(t, err)
	assert.False(t, res.(map[string]any)["success"].(bool))
}
