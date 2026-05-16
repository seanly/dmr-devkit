package skill

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// validateSkillContent checks frontmatter and size.
func validateSkillContent(content string, maxSize int) error {
	if len(content) > maxSize {
		return fmt.Errorf("skill content exceeds max size %d", maxSize)
	}
	fm, _, hasFM := splitFrontmatter(content)
	if !hasFM {
		return fmt.Errorf("skill must start with YAML frontmatter")
	}
	var front skillFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &front); err != nil {
		return fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	if strings.TrimSpace(front.Name) == "" {
		return fmt.Errorf("frontmatter missing 'name'")
	}
	if strings.TrimSpace(front.Description) == "" {
		return fmt.Errorf("frontmatter missing 'description'")
	}
	if typ := strings.TrimSpace(front.Type); typ != "" {
		if typ != "prompt" && typ != "agent" {
			return fmt.Errorf("frontmatter 'type' must be 'prompt' or 'agent'")
		}
	}
	if err := validateSkillSecrets(front.Secrets); err != nil {
		return err
	}
	return nil
}

// normalizeSkillGroup sets the group field in frontmatter to the desired value.
func normalizeSkillGroup(content string, group string) string {
	fm, body, hasFM := splitFrontmatter(content)
	if !hasFM {
		return content
	}
	var front map[string]any
	if err := yaml.Unmarshal([]byte(fm), &front); err != nil {
		return content
	}
	front["group"] = group
	newFM, err := yaml.Marshal(front)
	if err != nil {
		return content
	}
	return fmt.Sprintf("---\n%s---\n%s", string(newFM), body)
}
