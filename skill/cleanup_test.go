package skill

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupAutoSkills_ArchivesOld(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":                    []any{},
		"auto_create_path":         tmp,
		"max_auto_skills":          2,
		"archive_stale_after_days": 0, // archive anything
	})
	m := NewManager(cfg)

	// Create 4 old skills
	for i := 0; i < 4; i++ {
		dir := filepath.Join(tmp, "skill-"+string(rune('a'+i)))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\ndescription: d\n---\n"), 0o600))
		require.NoError(t, os.Chtimes(dir, time.Now().AddDate(0, 0, -10+i), time.Now().AddDate(0, 0, -10+i)))
	}

	m.cleanupAutoSkills()

	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	assert.Len(t, dirs, 2) // max_auto_skills = 2
	assert.Contains(t, dirs, "skill-c")
	assert.Contains(t, dirs, "skill-d")
}

func TestCleanupAutoSkills_RespectsMax(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg = ResolveConfig(cfg, map[string]any{
		"paths":                    []any{},
		"auto_create_path":         tmp,
		"max_auto_skills":          5,
		"archive_stale_after_days": 0,
	})
	m := NewManager(cfg)

	for i := 0; i < 3; i++ {
		dir := filepath.Join(tmp, "s"+string(rune('0'+i)))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\ndescription: d\n---\n"), 0o600))
	}

	m.cleanupAutoSkills()

	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	assert.Len(t, dirs, 3)
}
