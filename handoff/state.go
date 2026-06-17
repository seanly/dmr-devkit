package handoff

import (
	"encoding/json"
	"fmt"
	"time"
)

const SchemaVersion = 1

// State is structured task state (TaskState v1) persisted on tape as kind=task_state.
type State struct {
	SchemaVersion int               `json:"schema_version"`
	Goal          string            `json:"goal"`
	Completed     []CompletedItem   `json:"completed,omitempty"`
	Pending       []PendingItem     `json:"pending,omitempty"`
	Constraints   map[string]string `json:"constraints,omitempty"`
	Artifacts     []Artifact        `json:"artifacts,omitempty"`
	ActiveFiles   []string          `json:"active_files,omitempty"`
	LastAction    string            `json:"last_action,omitempty"`
	UpdatedAt     string            `json:"updated_at"`
	Source        string            `json:"source"`
}

type CompletedItem struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Step    int    `json:"step,omitempty"`
}

type PendingItem struct {
	ID        string   `json:"id"`
	Summary   string   `json:"summary"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type Artifact struct {
	Type  string `json:"type"`
	Ref   string `json:"ref"`
	Label string `json:"label,omitempty"`
}

// Validate checks required fields for v1 state.
func (s *State) Validate() error {
	if s == nil {
		return fmt.Errorf("state is nil")
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = SchemaVersion
	}
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", s.SchemaVersion)
	}
	if s.Goal == "" {
		return fmt.Errorf("goal is required")
	}
	if s.Source == "" {
		return fmt.Errorf("source is required")
	}
	return nil
}

// ToPayload converts State to a tape entry payload map.
func (s State) ToPayload() map[string]any {
	b, _ := json.Marshal(s)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// StateFromPayload parses task_state entry payload.
func StateFromPayload(payload map[string]any) (*State, error) {
	if payload == nil {
		return nil, fmt.Errorf("empty payload")
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = SchemaVersion
	}
	return &s, nil
}

// NewState creates a minimal valid state.
func NewState(goal, source string) State {
	return State{
		SchemaVersion: SchemaVersion,
		Goal:          goal,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Source:        source,
	}
}

// FormatPromptBlock renders state for LLM system injection.
func (s *State) FormatPromptBlock() string {
	if s == nil {
		return ""
	}
	var b fmtBuilder
	b.write("[TaskState v1]\n")
	b.write("goal: " + s.Goal + "\n")
	if len(s.Constraints) > 0 {
		b.write("constraints:\n")
		for k, v := range s.Constraints {
			b.write(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}
	if len(s.Pending) > 0 {
		b.write("pending:\n")
		for _, p := range s.Pending {
			b.write(fmt.Sprintf("  - %s\n", p.Summary))
		}
	}
	if len(s.Completed) > 0 {
		b.write("completed:\n")
		for _, c := range s.Completed {
			b.write(fmt.Sprintf("  - %s\n", c.Summary))
		}
	}
	if s.LastAction != "" {
		b.write("last_action: " + s.LastAction + "\n")
	}
	if len(s.ActiveFiles) > 0 {
		b.write("active_files: " + joinLimited(s.ActiveFiles, 10) + "\n")
	}
	if len(s.Artifacts) > 0 {
		b.write("artifacts:\n")
		for _, a := range s.Artifacts {
			ref := a.Ref
			if a.Label != "" {
				ref = a.Label + " (" + ref + ")"
			}
			b.write(fmt.Sprintf("  - %s: %s\n", a.Type, ref))
		}
	}
	return b.String()
}

type fmtBuilder struct {
	s string
}

func (b *fmtBuilder) write(x string) { b.s += x }
func (b *fmtBuilder) String() string { return b.s }

func joinLimited(ss []string, n int) string {
	if len(ss) > n {
		ss = ss[len(ss)-n:]
	}
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
