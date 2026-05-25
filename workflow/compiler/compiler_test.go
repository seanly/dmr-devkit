package compiler

import (
	"testing"

	"github.com/seanly/dmr-devkit/workflow/dsl"
)

func TestBuildGraph_Minimal(t *testing.T) {
	def := &dsl.WorkflowDef{
		Name:        "test",
		Description: "test",
		Stages: []dsl.StageDef{
			{ID: "s1", Agents: []dsl.AgentDef{{ID: "a1", Prompt: "hello"}}},
		},
	}
	cw, err := BuildGraph(def)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if cw.Graph == nil {
		t.Fatal("expected graph")
	}
	if len(cw.Graph.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
	if cw.Prompts["s1:a1"] == nil {
		t.Fatal("expected prompt template")
	}
}

func TestBuildGraph_Parallel(t *testing.T) {
	def := &dsl.WorkflowDef{
		Name:        "test",
		Description: "test",
		Stages: []dsl.StageDef{
			{
				ID:       "s1",
				Parallel: true,
				Agents: []dsl.AgentDef{
					{ID: "a1", Prompt: "p1"},
					{ID: "a2", Prompt: "p2"},
				},
			},
		},
	}
	cw, err := BuildGraph(def)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	joinID := "join:s1"
	if _, ok := cw.Graph.Nodes[joinID]; !ok {
		t.Fatalf("expected join node %s", joinID)
	}
}

func TestBuildGraph_Dependencies(t *testing.T) {
	def := &dsl.WorkflowDef{
		Name:        "test",
		Description: "test",
		Stages: []dsl.StageDef{
			{ID: "s1", Agents: []dsl.AgentDef{{ID: "a1", Prompt: "p1"}}},
			{ID: "s2", DependsOn: []string{"s1"}, Agents: []dsl.AgentDef{{ID: "a2", Prompt: "p2"}}},
		},
	}
	cw, err := BuildGraph(def)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	mergeID := "merge:s2"
	if _, ok := cw.Graph.Nodes[mergeID]; !ok {
		t.Fatalf("expected merge node %s", mergeID)
	}
}

func TestBuildGraph_ReturnTemplate(t *testing.T) {
	def := &dsl.WorkflowDef{
		Name:        "test",
		Description: "test",
		Stages: []dsl.StageDef{
			{ID: "s1", Agents: []dsl.AgentDef{{ID: "a1", Prompt: "p1"}}},
		},
		Return: dsl.ReturnDef{Template: "Result: {{.stages.s1.agents.a1.output}}"},
	}
	cw, err := BuildGraph(def)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if cw.ReturnT == nil {
		t.Fatal("expected return template")
	}
}

func TestBuildGraph_Invalid(t *testing.T) {
	def := &dsl.WorkflowDef{
		Name:        "",
		Description: "",
	}
	_, err := BuildGraph(def)
	if err == nil {
		t.Fatal("expected error for invalid def")
	}
}

func TestToposort(t *testing.T) {
	stages := []dsl.StageDef{
		{ID: "c", DependsOn: []string{"b"}, Agents: []dsl.AgentDef{{ID: "a", Prompt: "p"}}},
		{ID: "a", Agents: []dsl.AgentDef{{ID: "a", Prompt: "p"}}},
		{ID: "b", DependsOn: []string{"a"}, Agents: []dsl.AgentDef{{ID: "a", Prompt: "p"}}},
	}
	ordered, err := topoSort(stages)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	if len(ordered) != 3 {
		t.Fatalf("expected 3, got %d", len(ordered))
	}
	// a must come before b, b must come before c.
	aIdx, bIdx, cIdx := ordered[0], ordered[1], ordered[2]
	if stages[aIdx].ID != "a" || stages[bIdx].ID != "b" || stages[cIdx].ID != "c" {
		t.Fatalf("expected order a,b,c, got %s,%s,%s", stages[aIdx].ID, stages[bIdx].ID, stages[cIdx].ID)
	}
}

func TestToposort_Cycle(t *testing.T) {
	stages := []dsl.StageDef{
		{ID: "a", DependsOn: []string{"b"}, Agents: []dsl.AgentDef{{ID: "x", Prompt: "p"}}},
		{ID: "b", DependsOn: []string{"a"}, Agents: []dsl.AgentDef{{ID: "x", Prompt: "p"}}},
	}
	_, err := topoSort(stages)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestExtractYAML(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "fenced yaml",
			raw:  "Some text\n```yaml\nname: test\n```\nMore text",
			want: "name: test",
		},
		{
			name: "fenced generic",
			raw:  "```\nname: test\n```",
			want: "name: test",
		},
		{
			name: "plain",
			raw:  "name: test",
			want: "name: test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractYAML(tt.raw)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
