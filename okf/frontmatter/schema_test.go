package frontmatter

import (
	"testing"
)

func TestIsAttestedComputation(t *testing.T) {
	tests := []struct {
		typ  string
		want bool
	}{
		{"Attested Computation", true},
		{"attested computation", true},
		{"attested_computation", true},
		{"Attested_Computation", true},
		{"AttestedComputation", false},
		{"Metric", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			if got := IsAttestedComputation(tt.typ); got != tt.want {
				t.Errorf("IsAttestedComputation(%q) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

func TestValidateStatus(t *testing.T) {
	tests := []struct {
		status  string
		wantErr bool
	}{
		{"", false},
		{"draft", false},
		{"stable", false},
		{"deprecated", false},
		{"archived", true},
		{"DRAFT", true}, // 大小写敏感，SPEC 给定小写
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			m := &Meta{Status: tt.status}
			err := m.ValidateStatus()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStatus(%q) err = %v, wantErr %v", tt.status, err, tt.wantErr)
			}
		})
	}
}

func TestValidateStaleAfter(t *testing.T) {
	good := "2026-09-23"
	bad1 := "2026-9-23"
	bad2 := "2026/09/23"
	tests := []struct {
		name    string
		value   *string
		wantErr bool
	}{
		{"nil", nil, false},
		{"good", &good, false},
		{"bad month", &bad1, true},
		{"slash format", &bad2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Meta{StaleAfter: tt.value}
			err := m.ValidateStaleAfter()
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateGenerated(t *testing.T) {
	tests := []struct {
		name    string
		gen     *ActorEvent
		wantErr bool
	}{
		{"nil", nil, false},
		{"with by", &ActorEvent{By: "human:x"}, false},
		{"empty by", &ActorEvent{By: ""}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Meta{Generated: tt.gen}
			err := m.ValidateGenerated()
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSources(t *testing.T) {
	tests := []struct {
		name    string
		sources []Source
		wantErr bool
	}{
		{"empty", nil, false},
		{"with resource", []Source{{Resource: "https://example.com"}}, false},
		{"missing resource", []Source{{ID: "x"}}, true},
		{"mixed", []Source{{Resource: "ok"}, {ID: "bad"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Meta{Sources: tt.sources}
			err := m.ValidateSources()
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeriveTrustTier(t *testing.T) {
	tests := []struct {
		name string
		ver  VerifiedList
		want TrustTier
	}{
		{"none", nil, Unverified},
		{"machine only", VerifiedList{{By: "process:nightly"}}, MachineConfirmed},
		{"agent only", VerifiedList{{By: "reference_agent/gemini"}}, MachineConfirmed},
		{"human", VerifiedList{{By: "human:ahormati"}}, HumanReviewed},
		{"mixed human+process", VerifiedList{{By: "process:x"}, {By: "human:y"}}, HumanReviewed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Meta{Verified: tt.ver}
			if got := m.DeriveTrustTier(); got != tt.want {
				t.Errorf("= %q, want %q", got, tt.want)
			}
		})
	}
}
