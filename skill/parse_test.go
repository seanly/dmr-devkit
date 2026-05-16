package skill

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSkillMarkdown_MultilineDescription(t *testing.T) {
	raw := `---
name: test-skill
description: |
  Line one with colon: value
  Second line.
type: prompt
---
Body line.
`
	sk, err := ParseSkillMarkdown([]byte(raw), "/tmp/x/SKILL.md")
	require.NoError(t, err)
	assert.Equal(t, "test-skill", sk.Name)
	assert.Contains(t, sk.Description, "Line one with colon")
	assert.Contains(t, sk.Description, "Second line")
	assert.Equal(t, "prompt", sk.Type)
	assert.Equal(t, "Body line.", strings.TrimSpace(sk.Content))
}

func TestParseSkillMarkdown_Secrets(t *testing.T) {
	raw := `---
name: api
description: Call external API
secrets:
  - name: API_KEY
    description: Bearer token
    where: console.example.com
  - env: SECONDARY
    description: optional
    where: docs
---
Instructions.
`
	sk, err := ParseSkillMarkdown([]byte(raw), "/a/SKILL.md")
	require.NoError(t, err)
	require.Len(t, sk.Secrets, 2)
	assert.Equal(t, "API_KEY", sk.Secrets[0].Name)
	assert.Equal(t, "Bearer token", sk.Secrets[0].Description)
	assert.Equal(t, "SECONDARY", sk.Secrets[1].Name)
}

func TestParseSkillMarkdown_AgentSkill(t *testing.T) {
	raw := `---
name: researcher
description: Web search specialist
type: agent
when_to_use: Use for real-time web queries
model: cheap
max_iterations: 6
max_result_chars: 2000
tool_allowlist: [search, read_url]
subagents: [code_executor]
---
You are a research specialist.
`
	sk, err := ParseSkillMarkdown([]byte(raw), "/a/SKILL.md")
	require.NoError(t, err)
	assert.Equal(t, "researcher", sk.Name)
	assert.Equal(t, "agent", sk.Type)
	assert.Equal(t, "Use for real-time web queries", sk.WhenToUse)
	assert.Equal(t, "cheap", sk.Model)
	assert.Equal(t, 6, sk.MaxIterations)
	assert.Equal(t, 2000, sk.MaxResultChars)
	assert.Equal(t, []string{"search", "read_url"}, sk.ToolAllowlist)
	assert.Equal(t, []string{"code_executor"}, sk.Subagents)
	assert.Equal(t, "You are a research specialist.", strings.TrimSpace(sk.Content))
}

func TestParseSkillMarkdown_LegacyWorkflowBecomesAgent(t *testing.T) {
	raw := `---
name: deploy
description: Deploy pipeline
type: workflow
---
You are a deploy specialist.
`
	sk, err := ParseSkillMarkdown([]byte(raw), "/a/SKILL.md")
	require.NoError(t, err)
	assert.Equal(t, "agent", sk.Type, "legacy workflow type should be normalized to agent")
}

func TestParseSkillMarkdown_Defaults(t *testing.T) {
	raw := `---
name: minimal
description: Minimal skill
---
body
`
	sk, err := ParseSkillMarkdown([]byte(raw), "/a/SKILL.md")
	require.NoError(t, err)
	assert.Equal(t, "prompt", sk.Type)
	assert.Equal(t, "inherit", sk.Model)
	assert.Equal(t, 8, sk.MaxIterations)
	assert.Equal(t, 0, sk.MaxResultChars)
}

func TestValidateSkillContent_AgentWithSecrets(t *testing.T) {
	content := `---
name: wf
description: d
type: agent
secrets:
  - name: X
    where: w
---

You are a specialist.
`
	require.NoError(t, validateSkillContent(content, 65536))
}

func TestValidateSkillContent_Secrets(t *testing.T) {
	good := `---
name: x
description: d
secrets:
  - name: FOO
    description: foo
---
`
	require.NoError(t, validateSkillContent(good, 65536))

	badMissingName := `---
name: x
description: d
secrets:
  - description: only
    where: w
---
`
	require.Error(t, validateSkillContent(badMissingName, 65536))

	badNoDesc := `---
name: x
description: d
secrets:
  - name: FOO
---
`
	require.Error(t, validateSkillContent(badNoDesc, 65536))
}

func TestSecretsSummaryForPrompt_Truncates(t *testing.T) {
	var long []SkillSecret
	for i := 0; i < 50; i++ {
		long = append(long, SkillSecret{Name: "VAR", Description: "d", Where: "w"})
	}
	s := secretsSummaryForPrompt(long)
	assert.LessOrEqual(t, len([]rune(s)), maxSecretsSummaryRunes+4) // ellipsis
}
