package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/handoff"
	"github.com/seanly/dmr-devkit/provider"
)

func TestValidateCompactSummary(t *testing.T) {
	st := &handoff.State{Goal: "refactor handoff pipeline"}
	if !validateCompactSummary(st, "We refactored the handoff pipeline successfully") {
		t.Fatal("expected pass when summary contains goal token")
	}
	if validateCompactSummary(st, "unrelated summary without keywords") {
		t.Fatal("expected fail for unrelated summary")
	}
	if !validateCompactSummary(nil, "any summary") {
		t.Fatal("expected pass when no state but summary present")
	}
}

func TestValidateCompactSummaryWithLLM_Pass(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: `{"pass": true, "reason": "summary captures the goal"}`, Usage: &provider.Usage{TotalTokens: 10}},
	}}
	a := newSummarizerTestAgent(fake)
	st := &handoff.State{Goal: "refactor handoff pipeline"}
	pass, reason, err := validateCompactSummaryWithLLM(context.Background(), a.defaultChat, st, "We refactored the handoff pipeline", "tape1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pass {
		t.Fatalf("expected pass, got fail: %s", reason)
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestValidateCompactSummaryWithLLM_Fail(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: `{"pass": false, "reason": "goal is missing"}`, Usage: &provider.Usage{TotalTokens: 10}},
	}}
	a := newSummarizerTestAgent(fake)
	st := &handoff.State{Goal: "refactor handoff pipeline"}
	pass, reason, err := validateCompactSummaryWithLLM(context.Background(), a.defaultChat, st, "We had lunch", "tape1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pass {
		t.Fatal("expected fail")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestValidateCompactSummaryWithLLM_FallbackOnError(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		fmt.Errorf("network error"),
	}}
	a := newSummarizerTestAgent(fake)
	st := &handoff.State{Goal: "refactor handoff pipeline"}
	_, _, err := validateCompactSummaryWithLLM(context.Background(), a.defaultChat, st, "We refactored the handoff pipeline", "tape1")
	if err == nil {
		t.Fatal("expected error to trigger fallback")
	}
}

func TestValidateCompactSummaryWithLLM_NoState(t *testing.T) {
	pass, reason, err := validateCompactSummaryWithLLM(context.Background(), nil, nil, "any summary", "tape1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pass {
		t.Fatal("expected pass when state is nil and summary is non-empty")
	}
	if reason != "" {
		t.Fatalf("expected empty reason, got %q", reason)
	}
}

func TestParseSummaryJudgeResponse(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantPass bool
		wantErr  bool
	}{
		{"plain json", `{"pass": true, "reason": "ok"}`, true, false},
		{"json with markdown fence", "```json\n{\"pass\": false, \"reason\": \"missing\"}\n```", false, false},
		{"json with generic fence", "```\n{\"pass\": true, \"reason\": \"ok\"}\n```", true, false},
		{"empty", "", false, true},
		{"no json", "pass: true", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := parseSummaryJudgeResponse(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Pass != tc.wantPass {
				t.Fatalf("pass = %v, want %v", res.Pass, tc.wantPass)
			}
		})
	}
}
