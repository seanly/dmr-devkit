package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillTool_RereadsFileAfterEdit(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: alpha\ndescription: d\n---\nbody1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	cfg.AutoCreatePath = filepath.Join(tmp, "auto")
	m := NewManager(cfg)

	toolOut, err := m.skillHandler(nil, map[string]any{"name": "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolOut.(string), "body1") {
		t.Fatalf("expected body1 in %q", toolOut)
	}

	if err := os.WriteFile(path, []byte("---\nname: alpha\ndescription: d\n---\nbody2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	toolOut2, err := m.skillHandler(nil, map[string]any{"name": "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolOut2.(string), "body2") {
		t.Fatalf("expected body2 after edit, got %q", toolOut2)
	}
	if strings.Contains(toolOut2.(string), "body1") {
		t.Fatalf("should not contain stale body1: %q", toolOut2)
	}
}

func TestEnsureSkillsFresh_PicksUpNewSkillDir(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	cfg.AutoCreatePath = filepath.Join(tmp, "auto")
	m := NewManager(cfg)
	if len(m.skills) != 0 {
		t.Fatalf("expected no skills, got %d", len(m.skills))
	}

	skillDir := filepath.Join(tmp, "beta")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	// Sleep so mtime moves on fast FS (defensive)
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, []byte("---\nname: beta\ndescription: bd\n---\nbb\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m.ensureSkillsFresh()
	if len(m.skills) != 1 || m.skills[0].Name != "beta" {
		t.Fatalf("skills after refresh: %+v", m.skills)
	}

	raw := m.ComposeSystemPrompt(context.Background(), "")
	if !strings.Contains(raw, "beta") || !strings.Contains(raw, "bd") {
		t.Fatalf("system prompt: %q", raw)
	}
}

func TestSkillHandler_ReturnsStructuredOutput(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "alpha")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	path := filepath.Join(skillDir, "SKILL.md")
	content := `---
name: alpha
description: Alpha skill
type: prompt
---
Rule one.
Rule two.
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	m := NewManager(cfg)

	out, err := m.skillHandler(nil, map[string]any{"name": "alpha"})
	require.NoError(t, err)
	s := out.(string)
	assert.Contains(t, s, "# Skill Loaded: alpha")
	assert.Contains(t, s, "**Description:** Alpha skill")
	assert.Contains(t, s, "**Type:** prompt")
	assert.Contains(t, s, "## Instructions")
	assert.Contains(t, s, "Please follow the instructions below")
	assert.Contains(t, s, "Rule one.")
}

func TestBuildSystemPrompt_IncludesUsageInstructions(t *testing.T) {
	tmp := t.TempDir()
	promptDir := filepath.Join(tmp, "prompt-skill")
	agentDir := filepath.Join(tmp, "agent-skill")
	require.NoError(t, os.MkdirAll(promptDir, 0o755))
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptDir, "SKILL.md"), []byte("---\nname: prompt-skill\ndescription: A prompt skill\ngroup: core\n---\nbody\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "SKILL.md"), []byte("---\nname: agent-skill\ndescription: An agent skill\ngroup: core\ntype: agent\n---\nbody\n"), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	m := NewManager(cfg)

	prompt := m.ComposeSystemPrompt(context.Background(), "")
	assert.Contains(t, prompt, "## Skill Usage Instructions")
	assert.Contains(t, prompt, "skill(name=\"<skill_name>\")")
	assert.Contains(t, prompt, "delegate(skill=\"<specialist_name>\"")
	assert.Contains(t, prompt, "Do not silently ignore available skills")
}

func TestSkillUsageInstructions(t *testing.T) {
	promptSkills := []*Skill{{Name: "git-commit", Type: "prompt"}}
	agentSkills := []*Skill{{Name: "researcher", Type: "agent"}}
	usage := skillUsageInstructions(promptSkills, agentSkills)
	assert.Contains(t, usage, "Prompt skills")
	assert.Contains(t, usage, "Specialist agents")
	assert.Contains(t, usage, "Do not silently ignore available skills")

	assert.Empty(t, skillUsageInstructions(nil, nil))
}

func TestBuildSkillDelegationContext(t *testing.T) {
	sk := &Skill{
		Name:          "researcher",
		Description:   "Web search specialist",
		Type:          "agent",
		WhenToUse:     "Use for real-time web queries",
		Content:       "You are a research specialist.",
		ToolAllowlist: []string{"search", "read_url"},
	}
	ctx := tool.NewToolContext(context.Background(), "main-tape", "run-1")
	ctx.Workspace = "/workspace"

	out := buildSkillDelegationContext(ctx, sk, "Find Go 1.24 release notes")
	assert.Contains(t, out, "**researcher** specialist sub-agent")
	assert.Contains(t, out, "Find Go 1.24 release notes")
	assert.Contains(t, out, "main-tape")
	assert.Contains(t, out, "/workspace")
	assert.Contains(t, out, "Web search specialist")
	assert.Contains(t, out, "Use for real-time web queries")
	assert.Contains(t, out, "search, read_url")
	assert.Contains(t, out, "You are a research specialist.")
	assert.Contains(t, out, "## Response Guidelines")
}
