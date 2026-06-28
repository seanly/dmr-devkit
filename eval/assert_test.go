package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/handoff"
	"github.com/seanly/dmr-devkit/tape"
)

func TestEvaluateTapeTaskState(t *testing.T) {
	r := &Rubric{
		Name:      "test",
		PassScore: 0.8,
		Dimensions: []Dimension{{
			ID:     "state",
			Weight: 1,
			Assertions: []Assertion{{
				Type: "task_state_field_present", Field: "goal",
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewTaskStateEntry(handoff.NewState("fix bug", "heuristic").ToPayload()),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestEvaluateTapeLoopEvent(t *testing.T) {
	r := &Rubric{
		Name:      "handoff",
		PassScore: 0.8,
		Dimensions: []Dimension{{
			ID:     "handoff_event",
			Weight: 1,
			Assertions: []Assertion{{
				Type: "loop_event", Name: "loop:handoff", Field: "reason", Value: "preemptive",
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewEventEntry("loop:handoff", map[string]any{
			"reason": "preemptive", "anchor": "auto:preemptive:test",
		}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestEvaluateTapeTaskStateConstraint(t *testing.T) {
	st := handoff.NewState("Book Paris tickets", "heuristic")
	st.Constraints = map[string]string{"seat": "aisle"}
	r := &Rubric{
		Name:      "constraint",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "constraint",
			Weight: 1,
			Assertions: []Assertion{{
				Type: "task_state_constraint", Key: "seat", Value: "aisle",
			}},
		}},
	}
	card, err := EvaluateTape([]tape.TapeEntry{tape.NewTaskStateEntry(st.ToPayload())}, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestJudgeDimensionSkippedWithoutRunner(t *testing.T) {
	r := &Rubric{
		Name:      "judge-only",
		PassScore: 0.8,
		Dimensions: []Dimension{{
			ID:     "quality",
			Weight: 1,
			Judge:  &JudgeSpec{MinScore: 7, MaxScore: 10},
		}},
	}
	card, err := EvaluateTape(nil, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass when judge skipped: %+v", card)
	}
	if len(card.Results) != 1 || !card.Results[0].Skipped {
		t.Fatalf("expected skipped result: %+v", card.Results)
	}
}

func TestLLMJudgeParse(t *testing.T) {
	score, reason := parseJudgeResponse(`{"score": 8, "reason": "goal preserved"}`)
	if score != 8 || reason != "goal preserved" {
		t.Fatalf("parse = %v %q", score, reason)
	}
}

func TestEvaluateTapeWithMockJudge(t *testing.T) {
	r := &Rubric{
		Name:      "judge",
		PassScore: 0.7,
		Dimensions: []Dimension{{
			ID:     "quality",
			Weight: 1,
			Judge:  &JudgeSpec{MinScore: 7, MaxScore: 10, Prompt: "rate quality"},
		}},
	}
	opts := &Options{
		Judge: func(_ context.Context, _ []tape.TapeEntry, _ JudgeSpec) (float64, string, JudgeMeta, error) {
			return 9, "good", JudgeMeta{}, nil
		},
	}
	card, err := EvaluateTapeWithOptions(context.Background(), nil, r, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestAssertionNegate(t *testing.T) {
	r := &Rubric{
		Name:      "negate",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "no_shell",
			Weight: 1,
			Assertions: []Assertion{{
				Type: "tool_not_called", Name: "shell",
			}},
		}},
	}
	entries := []tape.TapeEntry{tape.NewMessageEntry(map[string]any{"role": "user", "content": "hello"})}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestAssertionAnyOf(t *testing.T) {
	r := &Rubric{
		Name:      "anyof",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "fs_or_shell",
			Weight: 1,
			Assertions: []Assertion{{
				AnyOf: []Assertion{
					{Type: "tool_called", Name: "fsGrep"},
					{Type: "tool_called", Name: "shell"},
				},
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":   "c1",
			"type": "function",
			"function": map[string]any{
				"name":      "fsGrep",
				"arguments": "{}",
			},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestDimensionPassScore(t *testing.T) {
	r := &Rubric{
		Name:      "partial",
		PassScore: 0.5,
		Dimensions: []Dimension{{
			ID:        "partial",
			Weight:    1,
			PassScore: 0.5,
			Assertions: []Assertion{
				{Type: "tool_called", Name: "fsGrep"},
				{Type: "tool_called", Name: "shell"},
			},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":   "c1",
			"type": "function",
			"function": map[string]any{
				"name":      "fsGrep",
				"arguments": "{}",
			},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass with dimension pass_score 0.5: %+v", card)
	}
}

func TestRegisterCustomAssertion(t *testing.T) {
	RegisterAssertion("always_true", func(_ []tape.TapeEntry, _ Assertion) (bool, string, error) {
		return true, "expected=true; actual=true", nil
	})
	r := &Rubric{
		Name:      "custom",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "custom",
			Weight: 1,
			Assertions: []Assertion{{
				Type: "always_true",
			}},
		}},
	}
	card, err := EvaluateTape(nil, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestToolCallOrder(t *testing.T) {
	r := &Rubric{
		Name:      "order",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "order",
			Weight: 1,
			Assertions: []Assertion{{
				Type:  "tool_call_order",
				Names: []string{"fsGrep", "fsRead"},
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":       "c1",
			"type":     "function",
			"function": map[string]any{"name": "fsGrep", "arguments": "{}"},
		}}),
		tape.NewToolCallEntry([]map[string]any{{
			"id":       "c2",
			"type":     "function",
			"function": map[string]any{"name": "fsRead", "arguments": "{}"},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestToolArgsMatch(t *testing.T) {
	r := &Rubric{
		Name:      "args",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "args",
			Weight: 1,
			Assertions: []Assertion{{
				Type:  "tool_args_match",
				Name:  "fsGrep",
				Key:   "pattern",
				Value: "TODO",
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":   "c1",
			"type": "function",
			"function": map[string]any{
				"name":      "fsGrep",
				"arguments": `{"pattern":"TODO"}`,
			},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestToolCallCount(t *testing.T) {
	r := &Rubric{
		Name:      "count",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "count",
			Weight: 1,
			Assertions: []Assertion{{
				Type: "tool_call_count",
				Name: "fsGrep",
				Min:  1,
				Max:  2,
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":       "c1",
			"type":     "function",
			"function": map[string]any{"name": "fsGrep", "arguments": "{}"},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestToolResultSuccess(t *testing.T) {
	r := &Rubric{
		Name:      "success",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "success",
			Weight: 1,
			Assertions: []Assertion{{
				Type: "tool_result_success",
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolResultEntry([]any{"ok"}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestStateConstraintPriority(t *testing.T) {
	st := handoff.NewState("Book tickets", "heuristic")
	st.Constraints = map[string]string{"seat": "aisle", "budget": "low"}
	r := &Rubric{
		Name:      "priority",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "priority",
			Weight: 1,
			Assertions: []Assertion{{
				Type:  "state_constraint_priority",
				Names: []string{"seat", "budget"},
			}},
		}},
	}
	card, err := EvaluateTape([]tape.TapeEntry{tape.NewTaskStateEntry(st.ToPayload())}, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass: %+v", card)
	}
}

func TestCompositeDimensionMin(t *testing.T) {
	r := &Rubric{
		Name:      "composite",
		PassScore: 0.8,
		Dimensions: []Dimension{{
			ID:          "state",
			Weight:      1,
			Aggregation: "min",
			SubDimensions: []Dimension{
				{ID: "a", Weight: 1, Assertions: []Assertion{{Type: "tool_called", Name: "fsGrep"}}},
				{ID: "b", Weight: 1, Assertions: []Assertion{{Type: "tool_called", Name: "shell"}}},
			},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":       "c1",
			"type":     "function",
			"function": map[string]any{"name": "fsGrep", "arguments": "{}"},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if card.Passed {
		t.Fatalf("expected fail because min aggregation with one failing sub: %+v", card)
	}
	if card.Score != 0 {
		t.Fatalf("expected score 0 for min aggregation: %+v", card)
	}
}

func TestCompositeDimensionWeightedSum(t *testing.T) {
	r := &Rubric{
		Name:      "composite",
		PassScore: 0.5,
		Dimensions: []Dimension{{
			ID:          "state",
			Weight:      1,
			Aggregation: "weighted_sum",
			SubDimensions: []Dimension{
				{ID: "a", Weight: 1, Assertions: []Assertion{{Type: "tool_called", Name: "fsGrep"}}},
				{ID: "b", Weight: 1, Assertions: []Assertion{{Type: "tool_called", Name: "shell"}}},
			},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":       "c1",
			"type":     "function",
			"function": map[string]any{"name": "fsGrep", "arguments": "{}"},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected pass with weighted_sum score 0.5 >= pass_score 0.5: %+v", card)
	}
}

func TestSuggestionForMissingTool(t *testing.T) {
	r := &Rubric{
		Name:      "suggest",
		PassScore: 1,
		Dimensions: []Dimension{{
			ID:     "tools",
			Weight: 1,
			Assertions: []Assertion{{
				Type:    "tool_called",
				Name:    "fs_grep",
				Because: "must search before reading",
			}},
		}},
	}
	entries := []tape.TapeEntry{
		tape.NewToolCallEntry([]map[string]any{{
			"id":       "c1",
			"type":     "function",
			"function": map[string]any{"name": "fsGrep", "arguments": "{}"},
		}}),
	}
	card, err := EvaluateTape(entries, r)
	if err != nil {
		t.Fatal(err)
	}
	if card.Passed {
		t.Fatalf("expected fail: %+v", card)
	}
	found := false
	for _, dr := range card.Results {
		for _, ar := range dr.AssertionResults {
			if strings.Contains(ar.Suggestion, "Did you mean") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected similarity suggestion: %+v", card.Results)
	}
}
