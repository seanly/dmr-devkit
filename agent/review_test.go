package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/tape"
)

type stubReviewDelegate struct {
	calls    []string
	critical bool
}

func (s *stubReviewDelegate) RunCritic(_ context.Context, _, skillName, _ string) (string, bool, error) {
	s.calls = append(s.calls, skillName)
	return "ok", s.critical, nil
}

func TestRunPostToolReviewChain(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, NopHooks(), Config{
		AgentPolicy: config.AgentConfig{
			Review: config.ReviewConfig{
				Enabled:    true,
				AfterTools: []string{"shell"},
				Chain:      []string{"critic-a", "critic-b"},
				MaxChainDepth: 2,
			},
		},
	})
	stub := &stubReviewDelegate{}
	a.SetReviewDelegate(stub)

	a.runPostToolReview(context.Background(), "main", 1, []string{"shell"}, []any{"output"})

	if len(stub.calls) != 2 {
		t.Fatalf("expected 2 critic calls, got %v", stub.calls)
	}
}

func TestRunPostToolReviewBlockFeedback(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, NopHooks(), Config{
		AgentPolicy: config.AgentConfig{
			Review: config.ReviewConfig{
				Enabled:           true,
				AfterTools:        []string{"shell"},
				Chain:             []string{"critic"},
				BlockOnCritical:   true,
			},
		},
	})
	stub := &stubReviewDelegate{critical: true}
	stub.calls = nil
	a.SetReviewDelegate(stub)
	review := a.runPostToolReview(context.Background(), "main", 1, []string{"shell"}, []any{"rm -rf /"})
	if review.Feedback == "" {
		t.Fatal("expected block feedback")
	}
	if !review.HardStop {
		t.Fatal("expected hard stop")
	}
	if !strings.Contains(review.Feedback, "[Review") {
		t.Fatalf("feedback = %q", review.Feedback)
	}
}

func TestParseCriticalVerdict(t *testing.T) {
	if !parseCriticalVerdict("[CRITICAL] bad command") {
		t.Fatal("expected critical")
	}
	if parseCriticalVerdict("all good") {
		t.Fatal("expected pass")
	}
}
