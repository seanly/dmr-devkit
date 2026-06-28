package eval

import (
	"context"
	"fmt"
	"math"
)

// StochasticRunFunc runs one stochastic iteration and returns the resulting ScoreCard.
type StochasticRunFunc func(ctx context.Context) (*ScoreCard, error)

// EvaluateStochastic runs a fixture multiple times and aggregates statistics.
// It returns a ScoreCard whose Score is the mean score and whose Passed field
// reflects the configured success-rate and score thresholds.
func EvaluateStochastic(ctx context.Context, spec *StochasticSpec, runFn StochasticRunFunc) (*ScoreCard, error) {
	if spec == nil {
		return nil, fmt.Errorf("nil stochastic spec")
	}
	if runFn == nil {
		return nil, fmt.Errorf("nil run function")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runs := spec.Runs
	if runs <= 0 {
		runs = 10
	}
	var scores []float64
	var passed int
	var minScore float64 = 1
	var maxScore float64
	for i := 0; i < runs; i++ {
		card, err := runFn(ctx)
		if err != nil {
			return nil, fmt.Errorf("run %d: %w", i+1, err)
		}
		if card == nil {
			continue
		}
		scores = append(scores, card.Score)
		if card.Passed {
			passed++
		}
		if card.Score < minScore {
			minScore = card.Score
		}
		if card.Score > maxScore {
			maxScore = card.Score
		}
	}
	if len(scores) == 0 {
		return nil, fmt.Errorf("no stochastic runs completed")
	}
	mean := mean(scores)
	stats := StochasticStats{
		Runs:        len(scores),
		SuccessRate: float64(passed) / float64(len(scores)),
		Mean:        mean,
		StdDev:      stddev(scores, mean),
		Min:         minScore,
		Max:         maxScore,
	}
	card := &ScoreCard{
		Passed:     stats.SuccessRate >= spec.SuccessRateThreshold && mean >= spec.ScoreThreshold,
		Score:      mean,
		PassScore:  spec.ScoreThreshold,
		Results:    []DimensionResult{},
		Statistics: &stats,
	}
	return card, nil
}

func mean(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stddev(values []float64, mean float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		d := v - mean
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(values)))
}
