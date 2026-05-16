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

func TestValidateSkillContent_WorkflowOK(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: workflow\n---\n\n### build\nprompt: Build\n"
	assert.NoError(t, validateSkillContent(content, 1024))
}

func TestValidateSkillContent_WorkflowInvalid(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: workflow\n---\nno step headings here"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "invalid workflow format")
}

func TestValidateSkillContent_WorkflowMissingPrompt(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: workflow\n---\n\n### build\nmodel: gpt-4\n"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "missing prompt")
}

func TestValidateSkillContent_WorkflowCycle(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: workflow\n---\n\n### a\nprompt: do a\ndepends_on: [b]\n\n### b\nprompt: do b\ndepends_on: [a]\n"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "cycle")
}

func TestValidateSkillContent_WorkflowDuplicateName(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: workflow\n---\n\n### build\nprompt: first\n\n### build\nprompt: second\n"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "duplicated")
}

func TestValidateSkillContent_WorkflowBadYAML(t *testing.T) {
	content := "---\nname: deploy\ndescription: Deploy pipeline\ntype: workflow\n---\n\n### build\nprompt: [unclosed\n"
	assert.ErrorContains(t, validateSkillContent(content, 1024), "invalid YAML")
}
