package dsl

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkflowDef_Validate(t *testing.T) {
	tests := []struct {
		name    string
		def     WorkflowDef
		wantErr bool
		wantMsg string
	}{
		{
			name:    "empty workflow",
			def:     WorkflowDef{},
			wantErr: true,
			wantMsg: "workflow.name is required",
		},
		{
			name: "missing description",
			def: WorkflowDef{
				Name: "test",
			},
			wantErr: true,
			wantMsg: "workflow.description is required",
		},
		{
			name: "no stages",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
			},
			wantErr: true,
			wantMsg: "workflow.stages must contain at least one stage",
		},
		{
			name: "valid minimal",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
				Stages: []StageDef{
					{ID: "s1", Description: "stage 1", Agents: []AgentDef{
						{ID: "a1", Prompt: "hello"},
					}},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate stage id",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
				Stages: []StageDef{
					{ID: "s1", Agents: []AgentDef{{ID: "a1", Prompt: "p"}}},
					{ID: "s1", Agents: []AgentDef{{ID: "a2", Prompt: "p"}}},
				},
			},
			wantErr: true,
			wantMsg: "duplicate stage id",
		},
		{
			name: "duplicate agent id in stage",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
				Stages: []StageDef{
					{ID: "s1", Agents: []AgentDef{
						{ID: "a1", Prompt: "p"},
						{ID: "a1", Prompt: "p"},
					}},
				},
			},
			wantErr: true,
			wantMsg: "duplicate agent id",
		},
		{
			name: "empty agent prompt",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
				Stages: []StageDef{
					{ID: "s1", Agents: []AgentDef{{ID: "a1", Prompt: ""}}},
				},
			},
			wantErr: true,
			wantMsg: "prompt is required",
		},
		{
			name: "unknown dependency",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
				Stages: []StageDef{
					{ID: "s1", Agents: []AgentDef{{ID: "a1", Prompt: "p"}}, DependsOn: []string{"nosuch"}},
				},
			},
			wantErr: true,
			wantMsg: "depends_on references unknown stage",
		},
		{
			name: "dependency cycle",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
				Stages: []StageDef{
					{ID: "s1", Agents: []AgentDef{{ID: "a1", Prompt: "p"}}, DependsOn: []string{"s2"}},
					{ID: "s2", Agents: []AgentDef{{ID: "a2", Prompt: "p"}}, DependsOn: []string{"s1"}},
				},
			},
			wantErr: true,
			wantMsg: "dependency cycle",
		},
		{
			name: "valid dependency chain",
			def: WorkflowDef{
				Name:        "test",
				Description: "test",
				Stages: []StageDef{
					{ID: "s1", Agents: []AgentDef{{ID: "a1", Prompt: "p"}}},
					{ID: "s2", Agents: []AgentDef{{ID: "a2", Prompt: "p"}}, DependsOn: []string{"s1"}},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.def.Validate()
			if !tt.wantErr {
				if len(errs) > 0 {
					t.Fatalf("expected no errors, got: %v", errs)
				}
				return
			}
			found := false
			for _, e := range errs {
				if contains(e, tt.wantMsg) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error containing %q, got: %v", tt.wantMsg, errs)
			}
		})
	}
}

func TestWorkflowDef_Validate_MultipleErrors(t *testing.T) {
	def := WorkflowDef{
		Name: "test",
		Stages: []StageDef{
			{ID: "s1"},
		},
	}
	errs := def.Validate()
	if len(errs) < 2 {
		t.Fatalf("expected multiple errors, got: %v", errs)
	}
}

func TestSchemaDoc(t *testing.T) {
	if SchemaDoc == "" {
		t.Fatal("SchemaDoc should not be empty")
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	src := WorkflowDef{
		Name:        "deep-research",
		Description: "Multi-source research",
		Input: InputSchema{
			Schema: map[string]string{"topic": "string"},
		},
		Stages: []StageDef{
			{
				ID:       "search",
				Parallel: true,
				Agents: []AgentDef{
					{ID: "docs", Prompt: "Search docs for {{.input.topic}}"},
					{ID: "community", Prompt: "Search community for {{.input.topic}}"},
				},
			},
			{
				ID:        "synthesis",
				DependsOn: []string{"search"},
				Agents: []AgentDef{
					{ID: "writer", Prompt: "Write report from {{.stages.search.agents.docs.output}}"},
				},
			},
		},
		Return: ReturnDef{Template: "# Report\n{{.stages.synthesis.agents.writer.output}}"},
	}

	data, err := yaml.Marshal(&src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var dst WorkflowDef
	if err := yaml.Unmarshal(data, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if dst.Name != src.Name {
		t.Fatalf("name mismatch: %q vs %q", dst.Name, src.Name)
	}
	if len(dst.Stages) != len(src.Stages) {
		t.Fatalf("stage count mismatch: %d vs %d", len(dst.Stages), len(src.Stages))
	}
	if !dst.Stages[0].Parallel {
		t.Fatal("expected parallel to round-trip")
	}
}

func contains(s, substr string) bool {
	return len(substr) <= len(s) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
