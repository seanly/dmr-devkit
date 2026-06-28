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
	outputs        []string
	index          int
	lastAllowed    []string
	lastModel      string
	lastMaxSteps   int
	lastContextJSON string
}

func (m *mockRuntimeAgent) AllModelInfos() []agent.ModelInfo                            { return nil }
func (m *mockRuntimeAgent) GetCurrentModelName(string) (string, string)                 { return "", "" }
func (m *mockRuntimeAgent) SwitchModel(string, string) error                            { return nil }
func (m *mockRuntimeAgent) CompactTape(context.Context, string) (string, error)         { return "", nil }
func (m *mockRuntimeAgent) CompactTapeWithFocus(context.Context, string, string) (string, error) { return "", nil }
func (m *mockRuntimeAgent) RestartProcess() error                                       { return nil }
func (m *mockRuntimeAgent) Run(context.Context, string, string, int32) (*agent.RunResult, error) {
	return nil, nil
}
func (m *mockRuntimeAgent) SetOnToolCall(func(agent.ToolCallEvent)) {}
func (m *mockRuntimeAgent) RunSubagent(context.Context, string, string, string, string, string, int) (*agent.SubagentResult, error) {
	return nil, nil
}
func (m *mockRuntimeAgent) SetDefaultTape(string) {}
func (m *mockRuntimeAgent) RunSubagentWithTools(_ context.Context, _, _, modelName, _, contextJSON string, maxSteps int, allowedTools []string, subagents []string) (*agent.SubagentResult, error) {
	m.lastModel = modelName
	m.lastMaxSteps = maxSteps
	m.lastAllowed = allowedTools
	m.lastContextJSON = contextJSON
	if m.index >= len(m.outputs) {
		return nil, fmt.Errorf("no more mock outputs")
	}
	out := m.outputs[m.index]
	m.index++
	return &agent.SubagentResult{Text: out}, nil
}

func TestHandleSkillDelegate(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "researcher")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	content := `---
name: researcher
description: Web search specialist
type: agent
when_to_use: Use for real-time web queries
model: cheap
max_iterations: 6
max_result_chars: 20
tool_allowlist: [search, read_url]
---
You are a research specialist.
`
	require.NoError(t, os.WriteFile(loc, []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	cfg.AutoCreatePath = filepath.Join(tmp, "auto")
	mgr := NewManager(cfg)

	mock := &mockRuntimeAgent{outputs: []string{"search results here"}}
	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": mock},
	}

	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "researcher",
		"task":  "Find Go 1.24 release notes",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.True(t, m["success"].(bool))
	assert.Equal(t, "search results here", m["output"].(string))
	assert.Equal(t, 1, mock.index)
	assert.Equal(t, "cheap", mock.lastModel)
	assert.Equal(t, 6, mock.lastMaxSteps)
	assert.Equal(t, []string{"search", "read_url"}, mock.lastAllowed)
	assert.Contains(t, mock.lastContextJSON, "research specialist")
}

func TestHandleSkillDelegate_Truncation(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "summarizer")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	content := `---
name: summarizer
description: Summarize text
type: agent
max_result_chars: 10
---
You are a summarizer.
`
	require.NoError(t, os.WriteFile(loc, []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	mgr := NewManager(cfg)

	mock := &mockRuntimeAgent{outputs: []string{"this is a very long result"}}
	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": mock},
	}

	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "summarizer",
		"task":  "Summarize this",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.True(t, m["success"].(bool))
	assert.Equal(t, "this is a \n[...truncated]", m["output"].(string))
}

func TestHandleSkillDelegate_NotFound(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	mgr := NewManager(cfg)

	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": &mockRuntimeAgent{}},
	}

	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "ghost",
		"task":  "do something",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.False(t, m["success"].(bool))
	assert.Contains(t, m["error"].(string), "not found")
}

func TestHandleSkillDelegate_NotAgent(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "plain")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: plain\ndescription: plain\ntype: prompt\n---\nbody\n"
	require.NoError(t, os.WriteFile(loc, []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	mgr := NewManager(cfg)

	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": &mockRuntimeAgent{}},
	}

	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "plain",
		"task":  "do something",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.False(t, m["success"].(bool))
	assert.Contains(t, m["error"].(string), "not an agent skill")
}

func TestHandleSkillDelegate_NoRuntimeAgent(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "researcher")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: researcher\ndescription: Research\ntype: agent\n---\nbody\n"
	require.NoError(t, os.WriteFile(loc, []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	mgr := NewManager(cfg)

	ctx := &tool.ToolContext{Tape: "main", State: map[string]any{}}
	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "researcher",
		"task":  "find something",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.False(t, m["success"].(bool))
	assert.Contains(t, m["error"].(string), "no runtime agent")
}

func TestHandleSkillDelegate_SubagentAllowlist_Allowed(t *testing.T) {
	tmp := t.TempDir()
	researcherDir := filepath.Join(tmp, "researcher")
	require.NoError(t, os.MkdirAll(researcherDir, 0o755))
	loc := filepath.Join(researcherDir, "SKILL.md")
	content := "---\nname: researcher\ndescription: Research\ntype: agent\n---\nbody\n"
	require.NoError(t, os.WriteFile(loc, []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	mgr := NewManager(cfg)

	mock := &mockRuntimeAgent{outputs: []string{"allowed result"}}
	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": mock, "subagent_allowlist": []string{"researcher"}},
	}

	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "researcher",
		"task":  "find something",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.True(t, m["success"].(bool))
	assert.Equal(t, "allowed result", m["output"].(string))
}

func TestHandleSkillDelegate_SubagentAllowlist_Denied(t *testing.T) {
	tmp := t.TempDir()
	researcherDir := filepath.Join(tmp, "researcher")
	summarizerDir := filepath.Join(tmp, "summarizer")
	require.NoError(t, os.MkdirAll(researcherDir, 0o755))
	require.NoError(t, os.MkdirAll(summarizerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(researcherDir, "SKILL.md"), []byte("---\nname: researcher\ndescription: Research\ntype: agent\n---\nbody\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(summarizerDir, "SKILL.md"), []byte("---\nname: summarizer\ndescription: Summarize\ntype: agent\n---\nbody\n"), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	mgr := NewManager(cfg)

	mock := &mockRuntimeAgent{outputs: []string{"should not reach"}}
	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": mock, "subagent_allowlist": []string{"researcher"}},
	}

	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "summarizer",
		"task":  "summarize this",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.False(t, m["success"].(bool))
	assert.Contains(t, m["error"].(string), "not allowed")
}

func TestHandleSkillDelegate_SubagentAllowlist_NoAllowlist(t *testing.T) {
	tmp := t.TempDir()
	researcherDir := filepath.Join(tmp, "researcher")
	require.NoError(t, os.MkdirAll(researcherDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(researcherDir, "SKILL.md"), []byte("---\nname: researcher\ndescription: Research\ntype: agent\n---\nbody\n"), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	mgr := NewManager(cfg)

	mock := &mockRuntimeAgent{outputs: []string{"allowed result"}}
	ctx := &tool.ToolContext{
		Tape:  "main",
		State: map[string]any{"_runtime_agent": mock},
	}

	res, err := mgr.handleDelegate(ctx, map[string]any{
		"skill": "researcher",
		"task":  "find something",
	})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.True(t, m["success"].(bool))
	assert.Equal(t, "allowed result", m["output"].(string))
}
