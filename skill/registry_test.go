package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverSkillsFromRoots_DedupesByName(t *testing.T) {
	tmp := t.TempDir()
	local := filepath.Join(tmp, "local", "book-to-insight-pipeline")
	clawhub := filepath.Join(tmp, "clawhub", "book-to-insight-pipeline")
	require.NoError(t, os.MkdirAll(local, 0o755))
	require.NoError(t, os.MkdirAll(clawhub, 0o755))

	content := `---
name: book-to-insight-pipeline
description: Book pipeline specialist
type: agent
when_to_use: Use for book insight workflows
---
Body from first path.
`
	require.NoError(t, os.WriteFile(filepath.Join(local, "SKILL.md"), []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(clawhub, "SKILL.md"), []byte(content+"\n"), 0o600))

	skills := discoverSkillsFromRoots([]string{local, clawhub})
	require.Len(t, skills, 1)
	assert.Equal(t, "book-to-insight-pipeline", skills[0].Name)
	assert.Contains(t, skills[0].Location, "local")
}

func TestSynthesizeDelegationTools_NoDuplicateNames(t *testing.T) {
	tmp := t.TempDir()
	auto := filepath.Join(tmp, "auto")
	skillDir := filepath.Join(auto, "book-to-insight-pipeline")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	content := `---
name: book-to-insight-pipeline
description: Book pipeline specialist
type: agent
---
Body.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600))

	cfg := DefaultConfig()
	cfg.Paths = []string{tmp}
	cfg.AutoCreatePath = auto // also scanned via resolvedRoots; must not duplicate
	mgr := NewManager(cfg)

	tools := mgr.synthesizeDelegationTools()
	names := make(map[string]int)
	for _, tl := range tools {
		names[tl.Spec.Name]++
	}
	assert.Equal(t, 1, names["delegate_book-to-insight-pipeline"])
}
