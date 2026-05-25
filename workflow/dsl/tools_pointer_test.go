package dsl

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAgentDefToolsOmitVsExplicitEmpty(t *testing.T) {
	const base = `name: w
description: d
stages:
  - id: s1
    agents:
      - id: a1
        prompt: hi
`
	var def WorkflowDef
	if err := yaml.Unmarshal([]byte(base), &def); err != nil {
		t.Fatal(err)
	}
	a := def.Stages[0].Agents[0]
	if a.Tools != nil {
		t.Fatalf("expected tools omitted -> nil pointer, got %+v", a.Tools)
	}

	const withEmpty = base + `        tools: []
`
	if err := yaml.Unmarshal([]byte(withEmpty), &def); err != nil {
		t.Fatal(err)
	}
	a = def.Stages[0].Agents[0]
	if a.Tools == nil {
		t.Fatal("expected explicit tools: [] -> non-nil pointer")
	}
	if len(*a.Tools) != 0 {
		t.Fatalf("expected empty slice, got %#v", *a.Tools)
	}
}
