package eval

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadRubric reads a rubric YAML file.
func LoadRubric(path string) (*Rubric, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Rubric
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.Name == "" {
		return nil, fmt.Errorf("rubric name required")
	}
	if r.PassScore <= 0 {
		r.PassScore = 0.8
	}
	return &r, nil
}
