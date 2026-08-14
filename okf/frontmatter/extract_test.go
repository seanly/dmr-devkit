package frontmatter

import (
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantType string
		wantBody string
	}{
		{
			name: "basic",
			input: `---
type: table
title: orders
---

## Schema

| id | type |
`,
			wantErr:  false,
			wantType: "table",
			wantBody: "## Schema",
		},
		{
			name:    "missing frontmatter",
			input:   "# Hello\n",
			wantErr: true,
		},
		{
			name: "empty body",
			input: `---
type: concept
---
`,
			wantErr:  false,
			wantType: "concept",
		},
		{
			name: "closing delim inside yaml value does not truncate (§4, Bug B)",
			input: `---
type: table
description: "a --- b"
---

Body here.
`,
			wantErr:  false,
			wantType: "table",
			wantBody: "Body here.",
		},
		{
			name: "unknown keys not rejected (§4.1)",
			input: `---
type: table
custom_field: hello
another: 42
---

Body.
`,
			wantErr:  false,
			wantType: "table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Extract(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Meta.Type != tt.wantType {
				t.Errorf("type = %q, want %q", res.Meta.Type, tt.wantType)
			}
			if tt.wantBody != "" && !strings.Contains(res.Body, tt.wantBody) {
				t.Errorf("body does not contain %q: %q", tt.wantBody, res.Body)
			}
		})
	}
}

func TestExtractVerified(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  TrustTier
	}{
		{
			name: "bare mapping (§5.2)",
			input: `---
type: Metric
verified: { by: human:ahormati, at: 2026-06-25T09:00:00Z }
---
`,
			want: HumanReviewed,
		},
		{
			name: "list with human verifier",
			input: `---
type: Metric
verified:
  - { by: human:ahormati, at: 2026-06-25T09:00:00Z }
  - { by: process:finance-nightly, at: 2026-06-26T02:00:00Z }
---
`,
			want: HumanReviewed,
		},
		{
			name: "machine only",
			input: `---
type: Metric
verified:
  - { by: process:finance-nightly, at: 2026-06-26T02:00:00Z }
---
`,
			want: MachineConfirmed,
		},
		{
			name: "absent => unverified",
			input: `---
type: Metric
---
`,
			want: Unverified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Extract(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := res.Meta.DeriveTrustTier(); got != tt.want {
				t.Errorf("trust tier = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractGenerated(t *testing.T) {
	input := `---
type: Metric
generated: { by: reference_agent/gemini-2.5-pro, at: 2026-06-20T22:53:05Z }
---
`
	res, err := Extract(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Meta.Generated == nil {
		t.Fatal("generated is nil")
	}
	if res.Meta.Generated.By != "reference_agent/gemini-2.5-pro" {
		t.Errorf("generated.by = %q", res.Meta.Generated.By)
	}
}

func TestExtractOKFVersion(t *testing.T) {
	input := `---
okf_version: "0.2"
---

# Bundle index
`
	res, err := Extract(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Meta.OKFVersion != "0.2" {
		t.Errorf("okf_version = %q, want %q", res.Meta.OKFVersion, "0.2")
	}
}
