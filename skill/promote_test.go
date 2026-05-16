package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCoreSkill(t *testing.T) {
	tmp := t.TempDir()
	corePath := filepath.Join(tmp, "core.md")
	extPath := filepath.Join(tmp, "ext.md")
	nonePath := filepath.Join(tmp, "none.md")

	_ = os.WriteFile(corePath, []byte("---\nname: c\ngroup: core\n---\n"), 0o600)
	_ = os.WriteFile(extPath, []byte("---\nname: e\ngroup: extended\n---\n"), 0o600)
	_ = os.WriteFile(nonePath, []byte("---\nname: n\n---\n"), 0o600)

	skCore, err := parseSkillFile(corePath)
	require.NoError(t, err)
	skExt, err := parseSkillFile(extPath)
	require.NoError(t, err)
	skNone, err := parseSkillFile(nonePath)
	require.NoError(t, err)

	assert.True(t, skillIsCore(skCore))
	assert.False(t, skillIsCore(skExt))
	assert.True(t, skillIsCore(skNone)) // backward compat: no group = core
}

func TestHandleSkillPromote(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	_ = os.WriteFile(loc, []byte("---\nname: my-skill\ndescription: d\ngroup: extended\n---\n"), 0o600)

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"paths": []any{tmp}, "auto_create_path": filepath.Join(tmp, "auto")})
	m := NewManager(cfg)

	res, err := m.handleSkillPromote(nil, map[string]any{"name": "my-skill"})
	require.NoError(t, err)
	assert.True(t, res.(map[string]any)["success"].(bool))
	assert.Equal(t, "core", res.(map[string]any)["group"])

	data, _ := os.ReadFile(loc)
	assert.Contains(t, string(data), "group: core")
}

func TestHandleSkillDemote(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	_ = os.WriteFile(loc, []byte("---\nname: my-skill\ndescription: d\ngroup: core\n---\n"), 0o600)

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"paths": []any{tmp}, "auto_create_path": filepath.Join(tmp, "auto")})
	m := NewManager(cfg)

	res, err := m.handleSkillDemote(nil, map[string]any{"name": "my-skill"})
	require.NoError(t, err)
	assert.True(t, res.(map[string]any)["success"].(bool))
	assert.Equal(t, "extended", res.(map[string]any)["group"])
}

func TestNormalizeSkillGroup_Replace(t *testing.T) {
	input := "---\nname: test\ngroup: extended\n---\nbody\n"
	out := normalizeSkillGroup(input, "core")
	assert.Contains(t, out, "group: core")
	assert.NotContains(t, out, "group: extended")
}

func TestHandleSkillList(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "listed")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: listed\ndescription: d\n---\n"), 0o600)

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"paths": []any{tmp}, "auto_create_path": filepath.Join(tmp, "auto")})
	m := NewManager(cfg)

	res, err := m.handleSkillList(nil, nil)
	require.NoError(t, err)
	assert.Contains(t, res.(string), "listed")
}

func TestHandleSkillEdit(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "edit-me")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	_ = os.WriteFile(loc, []byte("---\nname: edit-me\ndescription: old\n---\nold body\n"), 0o600)

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"paths": []any{tmp}, "auto_create_path": filepath.Join(tmp, "auto")})
	m := NewManager(cfg)

	newContent := "---\nname: edit-me\ndescription: new\n---\nnew body\n"
	res, err := m.handleSkillEdit(nil, map[string]any{"name": "edit-me", "content": newContent})
	require.NoError(t, err)
	assert.True(t, res.(map[string]any)["success"].(bool))

	data, _ := os.ReadFile(loc)
	assert.Contains(t, string(data), "new body")
}

func TestHandleSkillEdit_Patch(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "patch-me")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	_ = os.WriteFile(loc, []byte("---\nname: patch-me\ndescription: d\n---\nstep 1\nstep 2\n"), 0o600)

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"paths": []any{tmp}, "auto_create_path": filepath.Join(tmp, "auto")})
	m := NewManager(cfg)

	res, err := m.handleSkillEdit(nil, map[string]any{
		"name":       "patch-me",
		"old_string": "step 2",
		"new_string": "step 2 revised",
	})
	require.NoError(t, err)
	assert.True(t, res.(map[string]any)["success"].(bool))

	data, _ := os.ReadFile(loc)
	assert.Contains(t, string(data), "step 2 revised")
	assert.NotContains(t, string(data), "step 2\n")
}

func TestHandleSkillEdit_PatchAmbiguous(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "patch-amb")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	_ = os.WriteFile(loc, []byte("---\nname: patch-amb\ndescription: d\n---\nrepeat\nrepeat\n"), 0o600)

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"paths": []any{tmp}, "auto_create_path": filepath.Join(tmp, "auto")})
	m := NewManager(cfg)

	res, err := m.handleSkillEdit(nil, map[string]any{
		"name":       "patch-amb",
		"old_string": "repeat",
		"new_string": "once",
	})
	require.NoError(t, err)
	assert.False(t, res.(map[string]any)["success"].(bool))
}

func TestHandleSkillDelete(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "delete-me")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	loc := filepath.Join(skillDir, "SKILL.md")
	_ = os.WriteFile(loc, []byte("---\nname: delete-me\ndescription: d\n---\n"), 0o600)

	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{"paths": []any{tmp}, "auto_create_path": filepath.Join(tmp, "auto")})
	m := NewManager(cfg)

	res, err := m.handleSkillDelete(nil, map[string]any{"name": "delete-me"})
	require.NoError(t, err)
	assert.True(t, res.(map[string]any)["success"].(bool))

	_, err = os.Stat(loc)
	assert.True(t, os.IsNotExist(err))
}
