package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// skillFrontmatter is YAML frontmatter for SKILL.md.
type skillFrontmatter struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Type        string        `yaml:"type"`
	Group       string        `yaml:"group"`
	Secrets     []SkillSecret `yaml:"secrets"`

	// Agent skill fields
	WhenToUse      string   `yaml:"when_to_use,omitempty"`
	Model          string   `yaml:"model,omitempty"`
	MaxIterations  int      `yaml:"max_iterations,omitempty"`
	MaxResultChars int      `yaml:"max_result_chars,omitempty"`
	ToolAllowlist  []string `yaml:"tool_allowlist,omitempty"`
	Subagents      []string `yaml:"subagents,omitempty"`
}

// splitFrontmatter splits content into frontmatter YAML and body.
// Returns hasFM=false if no valid frontmatter block is found.
func splitFrontmatter(content string) (fm string, body string, hasFM bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return "", content, false
	}
	rest := trimmed[3:]
	if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	} else if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	}
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content, false
	}
	fm = rest[:idx]
	body = strings.TrimSpace(rest[idx+4:])
	return fm, body, true
}

// ParseSkillMarkdown parses a full SKILL.md: YAML frontmatter + body.
func ParseSkillMarkdown(data []byte, location string) (*Skill, error) {
	fm, body, ok := splitFrontmatter(string(data))
	if !ok {
		return nil, fmt.Errorf("skill %s: missing YAML frontmatter", location)
	}
	var front skillFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &front); err != nil {
		return nil, fmt.Errorf("skill %s: invalid YAML frontmatter: %w", location, err)
	}

	sk := &Skill{
		Location:       location,
		Name:           strings.TrimSpace(front.Name),
		Description:    strings.TrimSpace(front.Description),
		Type:           strings.TrimSpace(front.Type),
		Group:          strings.TrimSpace(front.Group),
		Content:        body,
		WhenToUse:      strings.TrimSpace(front.WhenToUse),
		Model:          strings.TrimSpace(front.Model),
		MaxIterations:  front.MaxIterations,
		MaxResultChars: front.MaxResultChars,
		ToolAllowlist:  front.ToolAllowlist,
		Subagents:      front.Subagents,
	}
	for _, sec := range front.Secrets {
		envName := sec.EnvName()
		sk.Secrets = append(sk.Secrets, SkillSecret{
			Name:        envName,
			Description: strings.TrimSpace(sec.Description),
			Where:       strings.TrimSpace(sec.Where),
		})
	}
	if sk.Name == "" {
		sk.Name = filepath.Base(filepath.Dir(location))
	}
	if sk.Type == "" {
		sk.Type = "prompt"
	}
	// Normalize legacy "workflow" to "agent"
	if sk.Type == "workflow" {
		sk.Type = "agent"
	}
	if sk.Model == "" {
		sk.Model = "inherit"
	}
	if sk.MaxIterations == 0 {
		sk.MaxIterations = 8
	}
	return sk, nil
}

func parseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSkillMarkdown(data, path)
}

// validateSkillSecrets checks optional secrets[] when present in validated content.
func validateSkillSecrets(secrets []SkillSecret) error {
	for i, sec := range secrets {
		envName := sec.EnvName()
		if envName == "" {
			return fmt.Errorf("secrets[%d]: missing name or env", i)
		}
		desc := strings.TrimSpace(sec.Description)
		where := strings.TrimSpace(sec.Where)
		if desc == "" && where == "" {
			return fmt.Errorf("secrets[%d]: need description or where", i)
		}
	}
	return nil
}
