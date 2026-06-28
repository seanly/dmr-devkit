package eval

import (
	"context"
	"testing"
)

func TestEvaluateStochastic(t *testing.T) {
	spec := &StochasticSpec{
		Runs:                 5,
		SuccessRateThreshold: 0.8,
		ScoreThreshold:       0.7,
	}
	i := 0
	card, err := EvaluateStochastic(context.Background(), spec, func(_ context.Context) (*ScoreCard, error) {
		i++
		return &ScoreCard{Passed: true, Score: 0.9}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !card.Passed {
		t.Fatalf("expected stochastic pass: %+v", card)
	}
	if card.Statistics == nil || card.Statistics.Runs != 5 {
		t.Fatalf("expected 5 runs, got %+v", card.Statistics)
	}
}

func TestEvaluateStochasticFailsThreshold(t *testing.T) {
	spec := &StochasticSpec{
		Runs:                 4,
		SuccessRateThreshold: 1.0,
		ScoreThreshold:       0.5,
	}
	i := 0
	card, err := EvaluateStochastic(context.Background(), spec, func(_ context.Context) (*ScoreCard, error) {
		i++
		passed := i != 1
		score := 0.9
		if !passed {
			score = 0.4
		}
		return &ScoreCard{Passed: passed, Score: score}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.Passed {
		t.Fatalf("expected stochastic fail due to success rate: %+v", card)
	}
}
