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
	sk, err := parseSkillMarkdown([]byte(raw), "/tmp/x/SKILL.md")
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
	sk, err := parseSkillMarkdown([]byte(raw), "/a/SKILL.md")
	require.NoError(t, err)
	require.Len(t, sk.Secrets, 2)
	assert.Equal(t, "API_KEY", sk.Secrets[0].Name)
	assert.Equal(t, "Bearer token", sk.Secrets[0].Description)
	assert.Equal(t, "SECONDARY", sk.Secrets[1].Name)
}

func TestValidateSkillContent_WorkflowWithSecrets(t *testing.T) {
	content := `---
name: wf
description: d
type: workflow
secrets:
  - name: X
    where: w
---

### step1
prompt: |
  do
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
