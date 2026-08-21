package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeName(t *testing.T) {
	assert.Equal(t, "hello-world", safeName("Hello World"))
	assert.Equal(t, "go-1-22", safeName("go 1.22"))
	assert.Equal(t, "my_skill", safeName("my_skill"))
}

func TestNormalizeSkillGroup(t *testing.T) {
	input := "---\nname: test\ngroup: core\n---\nbody\n"
	out := normalizeSkillGroup(input, "extended")
	assert.Contains(t, out, "group: extended")
	assert.NotContains(t, out, "group: core")
}

func TestHandleSkillCreate(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":            []any{},
		"auto_create_path": tmp,
		"allow_create":     true,
		"security_scan":    true,
		"max_skill_size":   65536,
	})
	m := NewManager(cfg)

	content := "---\nname: k8s-restart\ndescription: Restart pods\n---\n## Steps\n1. kubectl get pods\n"
	res, err := m.handleSkillCreate(nil, map[string]any{
		"name":    "k8s-restart",
		"content": content,
	})
	require.NoError(t, err)
	assert.True(t, res.(map[string]any)["success"].(bool))

	path := filepath.Join(tmp, "k8s-restart", "SKILL.md")
	assert.Equal(t, path, res.(map[string]any)["location"])

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "group: extended")
}

func TestHandleSkillCreate_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"allow_create": false})
	m := NewManager(cfg)
	res, err := m.handleSkillCreate(nil, map[string]any{"name": "x", "content": "---\nname: x\n---\n"})
	require.NoError(t, err)
	assert.False(t, res.(map[string]any)["success"].(bool))
}

func TestHandleSkillCreate_InvalidFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":            []any{},
		"auto_create_path": tmp,
		"allow_create":     true,
	})
	m := NewManager(cfg)
	res, err := m.handleSkillCreate(nil, map[string]any{
		"name":    "bad",
		"content": "no frontmatter here",
	})
	require.NoError(t, err)
	assert.False(t, res.(map[string]any)["success"].(bool))
}

func TestHandleSkillCreate_WithCategory(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":            []any{},
		"auto_create_path": tmp,
		"allow_create":     true,
		"security_scan":    false,
		"max_skill_size":   65536,
	})
	m := NewManager(cfg)

	content := "---\nname: deploy-k8s\ndescription: Deploy to k8s\n---\n## Steps\n1. kubectl apply\n"
	res, err := m.handleSkillCreate(nil, map[string]any{
		"name":     "deploy-k8s",
		"content":  content,
		"category": "devops",
	})
	require.NoError(t, err)
	mres := res.(map[string]any)
	assert.True(t, mres["success"].(bool))

	path := filepath.Join(tmp, "devops", "deploy-k8s", "SKILL.md")
	assert.Equal(t, path, mres["location"])

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "kubectl apply")
}

func TestHandleSkillCreate_WithFiles(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":            []any{},
		"auto_create_path": tmp,
		"allow_create":     true,
		"security_scan":    false,
		"max_skill_size":   65536,
	})
	m := NewManager(cfg)

	content := "---\nname: cyt-bundle\ndescription: A bundled skill\n---\n# Instructions\n"
	res, err := m.handleSkillCreate(nil, map[string]any{
		"name":    "cyt-bundle",
		"content": content,
		"files": map[string]any{
			"references/topology.md": "# Topology\n",
			"scripts/setup.sh":       "#!/bin/bash\necho ok\n",
		},
	})
	require.NoError(t, err)
	mres := res.(map[string]any)
	assert.True(t, mres["success"].(bool))

	skillDir := filepath.Join(tmp, "cyt-bundle")
	assert.Equal(t, filepath.Join(skillDir, "SKILL.md"), mres["location"])

	data, err := os.ReadFile(filepath.Join(skillDir, "references", "topology.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Topology\n", string(data))

	data, err = os.ReadFile(filepath.Join(skillDir, "scripts", "setup.sh"))
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/bash\necho ok\n", string(data))
}

func TestHandleSkillCreate_FilePathTraversal(t *testing.T) {
	tmp := t.TempDir()
	outside := filepath.Join(tmp, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":            []any{},
		"auto_create_path": tmp,
		"allow_create":     true,
		"security_scan":    false,
		"max_skill_size":   65536,
	})
	m := NewManager(cfg)

	content := "---\nname: evil\ndescription: bad\n---\n"
	res, err := m.handleSkillCreate(nil, map[string]any{
		"name":    "evil",
		"content": content,
		"files": map[string]any{
			"../outside/secret.txt": "pwned",
		},
	})
	require.NoError(t, err)
	mres := res.(map[string]any)
	assert.False(t, mres["success"].(bool))

	// Ensure nothing was written outside.
	_, err = os.ReadFile(filepath.Join(outside, "secret.txt"))
	assert.True(t, os.IsNotExist(err))
}

func TestHandleSkillCreate_TotalSizeLimit(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":            []any{},
		"auto_create_path": tmp,
		"allow_create":     true,
		"security_scan":    false,
		"max_skill_size":   100,
	})
	m := NewManager(cfg)

	content := "---\nname: big\ndescription: too big\n---\n"
	res, err := m.handleSkillCreate(nil, map[string]any{
		"name":    "big",
		"content": content,
		"files": map[string]any{
			"references/x.md": strings.Repeat("x", 200),
		},
	})
	require.NoError(t, err)
	mres := res.(map[string]any)
	assert.False(t, mres["success"].(bool))
	assert.Contains(t, mres["error"], "exceeds max size")

	// Ensure the skill directory was not left behind.
	_, err = os.Stat(filepath.Join(tmp, "big"))
	assert.True(t, os.IsNotExist(err))
}
