package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
