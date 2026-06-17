package handoff

import (
	"encoding/json"
	"fmt"
)

// Packet is structured sub-agent return (HandoffPacket v1).
type Packet struct {
	SchemaVersion int       `json:"schema_version"`
	Summary       string    `json:"summary"`
	Findings      []Finding `json:"findings,omitempty"`
	OpenItems     []string  `json:"open_items,omitempty"`
	Artifacts     []Artifact `json:"artifacts,omitempty"`
	TaskState     *State    `json:"task_state,omitempty"`
	Metrics       *Metrics  `json:"metrics,omitempty"`
	Text          string    `json:"text"`
}

type Finding struct {
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Evidence  string `json:"evidence,omitempty"`
}

type Metrics struct {
	Steps      int `json:"steps"`
	ToolCalls  int `json:"tool_calls,omitempty"`
}

// Validate checks packet v1.
func (p *Packet) Validate() error {
	if p == nil {
		return fmt.Errorf("packet is nil")
	}
	if p.SchemaVersion == 0 {
		p.SchemaVersion = SchemaVersion
	}
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", p.SchemaVersion)
	}
	if p.Text == "" && p.Summary == "" {
		return fmt.Errorf("text or summary required")
	}
	return nil
}

// ToPayload converts Packet to tape entry payload.
func (p Packet) ToPayload() map[string]any {
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// PacketFromPayload parses handoff_packet entry payload.
func PacketFromPayload(payload map[string]any) (*Packet, error) {
	if payload == nil {
		return nil, fmt.Errorf("empty payload")
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var p Packet
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.SchemaVersion == 0 {
		p.SchemaVersion = SchemaVersion
	}
	return &p, nil
}

// NewPacketFromOutput builds a minimal packet from sub-agent text output.
func NewPacketFromOutput(text string, state *State, steps, toolCalls int) *Packet {
	summary := text
	if len([]rune(summary)) > 500 {
		summary = string([]rune(summary)[:500])
	}
	return &Packet{
		SchemaVersion: SchemaVersion,
		Summary:       summary,
		TaskState:     state,
		Metrics:       &Metrics{Steps: steps, ToolCalls: toolCalls},
		Text:          text,
	}
}
