package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// emptySkillsMtime is a stable mtime when no skill files exist under configured roots.
var emptySkillsMtime = time.Unix(1, 0)

func maxFileMtimeUnderRoots(roots []string) time.Time {
	var max time.Time
	var found bool
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil
			}
			found = true
			if info.ModTime().After(max) {
				max = info.ModTime()
			}
			return nil
		})
	}
	if !found {
		return emptySkillsMtime
	}
	return max
}

func discoverSkillsFromRoots(paths []string) []*Skill {
	var skills []*Skill
	for _, dir := range paths {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.ToLower(filepath.Base(path)) != "skill.md" {
				return nil
			}
			s, err := parseSkillFile(path)
			if err != nil {
				return nil
			}
			skills = append(skills, s)
			return nil
		})
	}
	return skills
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func safeName(name string) string {
	name = strings.ToLower(name)
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	s := sb.String()
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

// ensureSkillsFresh rescans skill roots if files have changed.
func (m *Manager) ensureSkillsFresh() {
	m.ensureSkillsMu.Lock()
	defer m.ensureSkillsMu.Unlock()

	allRoots := append([]string{}, m.resolvedRoots...)
	if m.config.AutoCreatePath != "" {
		allRoots = append(allRoots, m.config.AutoCreatePath)
	}
	cur := maxFileMtimeUnderRoots(allRoots)
	if !cur.Equal(m.lastScanMtime) {
		m.skills = discoverSkillsFromRoots(allRoots)
		m.lastScanMtime = cur
	}
}

func (m *Manager) refreshSkills() {
	m.ensureSkillsMu.Lock()
	defer m.ensureSkillsMu.Unlock()
	allRoots := append([]string{}, m.resolvedRoots...)
	if m.config.AutoCreatePath != "" {
		allRoots = append(allRoots, m.config.AutoCreatePath)
	}
	m.skills = discoverSkillsFromRoots(allRoots)
	m.lastScanMtime = maxFileMtimeUnderRoots(allRoots)
}
