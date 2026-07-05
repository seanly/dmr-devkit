package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/provider"
)

func TestValidateCompactSummary(t *testing.T) {
	if !validateCompactSummary("We refactored the handoff pipeline successfully") {
		t.Fatal("expected pass for non-empty multi-word summary")
	}
	if validateCompactSummary("ok") {
		t.Fatal("expected fail for short summary")
	}
	if !validateCompactSummary("any summary with enough words") {
		t.Fatal("expected pass for sufficient length summary")
	}
}

func TestValidateCompactSummaryWithLLM_Pass(t *testing.T) {
	fake := &summarizerFakeClient{completionQueue: []any{
		&provider.ChatResponse{Text: `{"pass": true, "reason": "summary captures the goal"}`, Usage: &provider.Usage{TotalTokens: 10}},
	}}
	a := newSummarizerTestAgent(fake)
	pass, reason, err := validateCompactSummaryWithLLM(context.Background(), a.defaultChat, "We refactored the handoff pipeline", "tape1")
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
	pass, reason, err := validateCompactSummaryWithLLM(context.Background(), a.defaultChat, "We had lunch", "tape1")
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
	_, _, err := validateCompactSummaryWithLLM(context.Background(), a.defaultChat, "We refactored the handoff pipeline", "tape1")
	if err == nil {
		t.Fatal("expected error to trigger fallback")
	}
}

func TestValidateCompactSummaryWithLLM_NoClient(t *testing.T) {
	pass, reason, err := validateCompactSummaryWithLLM(context.Background(), nil, "any summary", "tape1")
	if err == nil {
		t.Fatal("expected error when client is nil")
	}
	if pass {
		t.Fatal("expected fail")
	}
	_ = reason
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
