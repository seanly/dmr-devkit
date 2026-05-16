package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cleanupAutoSkills archives old auto-created skills when count exceeds max.
func (m *Manager) cleanupAutoSkills() {
	if m.config.AutoCreatePath == "" || m.config.MaxAutoSkills <= 0 {
		return
	}
	type skillInfo struct {
		name    string
		path    string
		modTime time.Time
	}
	var skills []skillInfo
	_ = filepath.WalkDir(m.config.AutoCreatePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Base(path)) != "skill.md" {
			return nil
		}
		dir := filepath.Dir(path)
		info, err := os.Stat(dir)
		if err != nil {
			return nil
		}
		skills = append(skills, skillInfo{name: filepath.Base(dir), path: dir, modTime: info.ModTime()})
		return nil
	})
	if len(skills) <= m.config.MaxAutoSkills {
		return
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].modTime.Before(skills[j].modTime)
	})
	toArchive := skills[:len(skills)-m.config.MaxAutoSkills]
	archiveDir := filepath.Join(m.config.AutoCreatePath, "..", "archive")
	_ = os.MkdirAll(archiveDir, 0o755)
	cutoff := time.Now().AddDate(0, 0, -m.config.ArchiveStaleDays)
	for _, sk := range toArchive {
		if sk.modTime.After(cutoff) {
			continue // not stale yet
		}
		dst := filepath.Join(archiveDir, sk.name)
		_ = os.Rename(sk.path, dst)
	}
}
