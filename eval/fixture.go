package eval

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Fixture describes an eval scenario with a recorded tape and rubric.
type Fixture struct {
	Name   string `yaml:"name"`
	Tape   string `yaml:"tape"`   // tape name label (informational)
	Rubric string `yaml:"rubric"` // path to rubric YAML
	TapeFile string `yaml:"tape_file"` // path to JSON entries file
}

// LoadFixture reads a fixture YAML file.
func LoadFixture(path string) (*Fixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Fixture
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.Rubric == "" {
		return nil, fmt.Errorf("fixture rubric path required")
	}
	if f.TapeFile == "" {
		return nil, fmt.Errorf("fixture tape_file required")
	}
	if f.Name == "" {
		f.Name = path
	}
	return &f, nil
}

// RunFixture evaluates a fixture tape file against its rubric.
func RunFixture(fixturePath string) (*ScoreCard, error) {
	fix, err := LoadFixture(fixturePath)
	if err != nil {
		return nil, err
	}
	rubric, err := LoadRubric(fix.Rubric)
	if err != nil {
		return nil, fmt.Errorf("rubric: %w", err)
	}
	entries, err := LoadTapeEntries(fix.TapeFile)
	if err != nil {
		return nil, fmt.Errorf("tape: %w", err)
	}
	return EvaluateTape(entries, rubric)
}

// WriteFixture writes a fixture YAML file.
func WriteFixture(path string, f *Fixture) error {
	if f == nil {
		return fmt.Errorf("nil fixture")
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
