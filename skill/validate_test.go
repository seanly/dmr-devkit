package skill

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSkillContent_OK(t *testing.T) {
	content := "---\nname: test\ndescription: A test skill\n---\nbody"
	assert.NoError(t, validateSkillContent(content, 1024))
}

func TestValidateSkillContent_MissingFrontmatter(t *testing.T) {
	content := "no frontmatter"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "must start with YAML frontmatter")
}

func TestValidateSkillContent_MissingName(t *testing.T) {
	content := "---\ndescription: A test skill\n---\nbody"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "missing 'name'")
}

func TestValidateSkillContent_MissingDescription(t *testing.T) {
	content := "---\nname: test\n---\nbody"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "missing 'description'")
}

func TestValidateSkillContent_TooLarge(t *testing.T) {
	content := "---\nname: test\ndescription: d\n---\n" + strings.Repeat("x", 100)
	assert.ErrorContains(t, validateSkillContent(content, 50), "exceeds max size")
}

func TestValidateSkillContent_AgentOK(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: agent\n---\n\nYou are a deploy specialist."
	assert.NoError(t, validateSkillContent(content, 1024))
}

func TestValidateSkillContent_InvalidType(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: unknown\n---\nbody"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "'type' must be 'prompt' or 'agent'")
}
