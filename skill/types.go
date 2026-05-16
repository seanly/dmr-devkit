package skill

import (
	"strings"
)

// Skill represents a discoverable skill (SKILL.md).
type Skill struct {
	Name        string
	Description string
	Type        string
	Group       string
	Secrets     []SkillSecret
	Content     string
	Location    string
}

// SkillSecret is one declared secret/env need (documentation only).
type SkillSecret struct {
	Name        string `yaml:"name"`
	Env         string `yaml:"env"`
	Description string `yaml:"description"`
	Where       string `yaml:"where"`
}

// EnvName returns the target environment variable name (name or env).
func (s SkillSecret) EnvName() string {
	n := strings.TrimSpace(s.Name)
	if n != "" {
		return n
	}
	return strings.TrimSpace(s.Env)
}

// skillIsCore reports whether the skill is in the core group (default when group is empty).
func skillIsCore(s *Skill) bool {
	g := strings.TrimSpace(s.Group)
	if g == "" {
		return true
	}
	return strings.EqualFold(g, "core")
}

// maxSecretsSummaryRunes limits injected <secrets> text in system prompt.
const maxSecretsSummaryRunes = 400

// secretsSummaryForPrompt returns a compact comma-separated list of env names, or empty.
func secretsSummaryForPrompt(secrets []SkillSecret) string {
	if len(secrets) == 0 {
		return ""
	}
	var b strings.Builder
	total := 0
	for i, sec := range secrets {
		name := strings.TrimSpace(sec.Name)
		if name == "" {
			continue
		}
		if i > 0 && b.Len() > 0 {
			if total+1 > maxSecretsSummaryRunes {
				break
			}
			b.WriteByte(',')
			total++
		}
		for _, r := range name {
			if total >= maxSecretsSummaryRunes {
				b.WriteString("…")
				return b.String()
			}
			b.WriteRune(r)
			total++
		}
	}
	return b.String()
}
