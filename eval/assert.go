package eval

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/seanly/dmr-devkit/handoff"
	"github.com/seanly/dmr-devkit/tape"
)

// EvaluateTape runs deterministic assertions in rubric against tape entries.
// LLM judge dimensions are skipped (excluded from score) unless Options.Judge is set.
func EvaluateTape(entries []tape.TapeEntry, rubric *Rubric) (*ScoreCard, error) {
	return EvaluateTapeWithOptions(context.Background(), entries, rubric, nil)
}

// EvaluateTapeWithOptions evaluates a tape with optional LLM judge support.
func EvaluateTapeWithOptions(ctx context.Context, entries []tape.TapeEntry, rubric *Rubric, opts *Options) (*ScoreCard, error) {
	if rubric == nil {
		return nil, fmt.Errorf("nil rubric")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	card := &ScoreCard{
		Rubric:    rubric.Name,
		PassScore: rubric.PassScore,
		Results:   make([]DimensionResult, 0, len(rubric.Dimensions)),
	}
	var weighted float64
	var totalWeight float64
	for _, dim := range rubric.Dimensions {
		w := dim.Weight
		if w <= 0 {
			w = 1
		}
		res, err := evalDimensionResult(ctx, entries, dim, opts, w)
		if err != nil {
			return nil, fmt.Errorf("dimension %q: %w", dim.ID, err)
		}
		card.Results = append(card.Results, res)
		if res.Skipped {
			continue
		}
		weighted += res.Score * w
		totalWeight += w
	}
	if totalWeight > 0 {
		card.Score = weighted / totalWeight
	} else if len(card.Results) > 0 {
		card.Score = 1
	}
	card.Passed = card.Score >= rubric.PassScore
	return card, nil
}

func evalDimensionResult(ctx context.Context, entries []tape.TapeEntry, dim Dimension, opts *Options, weight float64) (DimensionResult, error) {
	res := DimensionResult{ID: dim.ID, Weight: weight}

	if dim.Judge != nil && len(dim.Assertions) == 0 {
		if opts == nil || opts.Judge == nil {
			res.Skipped = true
			res.Passed = true
			res.Score = 1
			res.Detail = "judge skipped (pass --judge-model to score)"
			return res, nil
		}
		score, detail, err := opts.Judge(ctx, entries, *dim.Judge)
		if err != nil {
			return res, err
		}
		res.Score = normalizeJudgeScore(score, dim.Judge)
		res.Passed = res.Score >= judgePassThreshold(dim.Judge)
		res.Detail = detail
		return res, nil
	}

	score, detail, err := evalDimension(entries, dim)
	if err != nil {
		return res, err
	}
	res.Score = score
	res.Passed = score >= 1.0

	if dim.Judge != nil {
		if opts == nil || opts.Judge == nil {
			res.Skipped = true
			res.Passed = true
			res.Detail = detail + "; judge skipped (pass --judge-model to score)"
			return res, nil
		}
		jScore, jDetail, err := opts.Judge(ctx, entries, *dim.Judge)
		if err != nil {
			return res, err
		}
		jNorm := normalizeJudgeScore(jScore, dim.Judge)
		res.Score = (score + jNorm) / 2
		res.Passed = res.Score >= judgePassThreshold(dim.Judge) && score >= 1.0
		res.Detail = detail + "; judge: " + jDetail
		return res, nil
	}

	res.Detail = detail
	return res, nil
}

func judgePassThreshold(spec *JudgeSpec) float64 {
	if spec == nil || spec.MinScore <= 0 {
		return 1.0
	}
	max := spec.MaxScore
	if max <= spec.MinScore {
		max = 10
	}
	return float64(spec.MinScore) / float64(max)
}

func normalizeJudgeScore(score float64, spec *JudgeSpec) float64 {
	if spec == nil {
		return score
	}
	max := spec.MaxScore
	if max <= 0 {
		max = 10
	}
	if score > 1 && score <= float64(max) {
		return score / float64(max)
	}
	if score > float64(max) {
		return 1
	}
	if score < 0 {
		return 0
	}
	return score
}

func evalDimension(entries []tape.TapeEntry, dim Dimension) (float64, string, error) {
	if len(dim.Assertions) == 0 {
		if dim.Judge != nil {
			return 0, "judge pending", nil
		}
		return 1, "no assertions", nil
	}
	passed := 0
	for _, a := range dim.Assertions {
		ok, err := evalAssertion(entries, a)
		if err != nil {
			return 0, "", err
		}
		if ok {
			passed++
		}
	}
	score := float64(passed) / float64(len(dim.Assertions))
	return score, fmt.Sprintf("%d/%d assertions passed", passed, len(dim.Assertions)), nil
}

func evalAssertion(entries []tape.TapeEntry, a Assertion) (bool, error) {
	switch a.Type {
	case "task_state_field_present":
		st := handoff.LatestState(entries)
		if st == nil {
			return false, nil
		}
		switch a.Field {
		case "goal":
			return st.Goal != "", nil
		default:
			return false, fmt.Errorf("unknown task_state field %q", a.Field)
		}
	case "tool_called":
		return countToolCalls(entries, a.Name) >= max1(a.Min), nil
	case "tool_not_called":
		return countToolCalls(entries, a.Name) == 0, nil
	case "tape_entry_kind":
		n := countKind(entries, a.Name)
		return n >= max1(a.Min), nil
	case "anchor_exists":
		for _, e := range entries {
			if e.Kind != "anchor" {
				continue
			}
			name, _ := e.Payload["name"].(string)
			if name == a.Name {
				return true, nil
			}
		}
		return false, nil
	case "loop_event":
		for _, e := range entries {
			if e.Kind != "event" {
				continue
			}
			name, _ := e.Payload["name"].(string)
			if a.Name != "" && name != a.Name {
				continue
			}
			data, _ := e.Payload["data"].(map[string]any)
			if a.Field == "" {
				return true, nil
			}
			if matchEventField(data[a.Field], a.Value) {
				return true, nil
			}
		}
		return false, nil
	case "tape_must_not_match":
		re, err := regexp.Compile(a.Regex)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if e.Kind != "message" {
				continue
			}
			role, _ := e.Payload["role"].(string)
			if a.Role != "" && role != a.Role {
				continue
			}
			content, _ := e.Payload["content"].(string)
			if re.MatchString(content) {
				return false, nil
			}
		}
		return true, nil
	case "task_state_constraint":
		st := handoff.LatestState(entries)
		if st == nil || st.Constraints == nil {
			return false, nil
		}
		v, ok := st.Constraints[a.Key]
		return ok && v == a.Value, nil
	default:
		return false, fmt.Errorf("unknown assertion type %q", a.Type)
	}
}

func matchEventField(got any, want string) bool {
	if want == "" {
		return got != nil
	}
	switch v := got.(type) {
	case string:
		return v == want
	case bool:
		wantBool, err := strconv.ParseBool(want)
		return err == nil && v == wantBool
	case float64:
		wantNum, err := strconv.ParseFloat(want, 64)
		return err == nil && v == wantNum
	case int:
		wantNum, err := strconv.Atoi(want)
		return err == nil && v == wantNum
	default:
		return fmt.Sprint(got) == want
	}
}

func max1(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}

func countKind(entries []tape.TapeEntry, kind string) int {
	n := 0
	for _, e := range entries {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func countToolCalls(entries []tape.TapeEntry, name string) int {
	n := 0
	for _, e := range entries {
		if e.Kind != "tool_call" {
			continue
		}
		calls, ok := tape.ExtractToolCalls(e.Payload)
		if !ok {
			continue
		}
		for _, c := range calls {
			if name == "" || c.Name == name {
				n++
			}
		}
	}
	return n
}

// FormatScoreCard returns a human-readable summary.
func FormatScoreCard(card *ScoreCard) string {
	if card == nil {
		return ""
	}
	status := "FAIL"
	if card.Passed {
		status = "PASS"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s rubric=%q score=%.2f pass_score=%.2f\n", status, card.Rubric, card.Score, card.PassScore)
	for _, r := range card.Results {
		skip := ""
		if r.Skipped {
			skip = " skipped"
		}
		fmt.Fprintf(&b, "  - %s: score=%.2f passed=%v%s %s\n", r.ID, r.Score, r.Passed, skip, r.Detail)
	}
	return b.String()
}
