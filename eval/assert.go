package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/seanly/dmr-devkit/handoff"
	"github.com/seanly/dmr-devkit/tape"
)

// AssertionFunc evaluates a single assertion against tape entries.
// The returned string is a human-readable detail (expected vs actual) for diagnostics.
type AssertionFunc func(entries []tape.TapeEntry, a Assertion) (bool, string, error)

var assertionRegistry = map[string]AssertionFunc{}

// RegisterAssertion registers a custom assertion type.
// Built-in assertions are registered in init().
func RegisterAssertion(name string, fn AssertionFunc) {
	assertionRegistry[name] = fn
}

func init() {
	RegisterAssertion("task_state_field_present", assertTaskStateFieldPresent)
	RegisterAssertion("task_state_constraint", assertTaskStateConstraint)
	RegisterAssertion("tool_called", assertToolCalled)
	RegisterAssertion("tool_not_called", assertToolNotCalled)
	RegisterAssertion("tape_entry_kind", assertTapeEntryKind)
	RegisterAssertion("anchor_exists", assertAnchorExists)
	RegisterAssertion("loop_event", assertLoopEvent)
	RegisterAssertion("tape_must_not_match", assertTapeMustNotMatch)
	RegisterAssertion("tool_call_order", assertToolCallOrder)
	RegisterAssertion("tool_args_match", assertToolArgsMatch)
	RegisterAssertion("tool_result_success", assertToolResultSuccess)
	RegisterAssertion("tool_call_count", assertToolCallCount)
	RegisterAssertion("context_key_preserved", assertContextKeyPreserved)
	RegisterAssertion("state_constraint_priority", assertStateConstraintPriority)
	RegisterAssertion("event_order", assertEventOrder)
}

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
	if opts != nil && opts.Cost == nil {
		opts.Cost = &CostTracker{}
	}
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
	if opts != nil && opts.Cost != nil {
		card.Cost = opts.Cost.Snapshot()
	}
	return card, nil
}

func evalDimensionResult(ctx context.Context, entries []tape.TapeEntry, dim Dimension, opts *Options, weight float64) (DimensionResult, error) {
	res := DimensionResult{ID: dim.ID, Weight: weight}

	// Composite dimension: recurse into sub_dimensions.
	if len(dim.SubDimensions) > 0 {
		return evalCompositeDimension(ctx, entries, dim, opts, weight)
	}

	if dim.Judge != nil && len(dim.Assertions) == 0 {
		if opts == nil || opts.Judge == nil {
			res.Skipped = true
			res.Passed = true
			res.Score = 1
			res.Detail = "judge skipped (pass --judge-model to score)"
			return res, nil
		}
		score, detail, meta, err := opts.Judge(ctx, entries, *dim.Judge)
		if err != nil {
			return res, err
		}
		res.Score = normalizeJudgeScore(score, dim.Judge)
		res.Passed = res.Score >= judgePassThreshold(dim.Judge)
		res.Detail = detail
		res.JudgeMeta = &meta
		return res, nil
	}

	score, detail, assertionResults, err := evalDimension(entries, dim)
	if err != nil {
		return res, err
	}
	res.Score = score
	res.Passed = score >= dimensionPassScore(dim)
	res.AssertionResults = assertionResults

	if dim.Judge != nil {
		if opts == nil || opts.Judge == nil {
			res.Skipped = true
			res.Passed = true
			res.Detail = detail + "; judge skipped (pass --judge-model to score)"
			return res, nil
		}
		jScore, jDetail, meta, err := opts.Judge(ctx, entries, *dim.Judge)
		if err != nil {
			return res, err
		}
		jNorm := normalizeJudgeScore(jScore, dim.Judge)
		res.Score = (score + jNorm) / 2
		res.Passed = res.Score >= judgePassThreshold(dim.Judge) && score >= 1.0
		res.Detail = detail + "; judge: " + jDetail
		res.JudgeMeta = &meta
		return res, nil
	}

	res.Detail = detail
	return res, nil
}

func evalCompositeDimension(ctx context.Context, entries []tape.TapeEntry, dim Dimension, opts *Options, weight float64) (DimensionResult, error) {
	res := DimensionResult{ID: dim.ID, Weight: weight}
	var subResults []DimensionResult
	var weighted float64
	var totalWeight float64
	anyFailed := false
	var minScore float64 = 1

	for _, sub := range dim.SubDimensions {
		sw := sub.Weight
		if sw <= 0 {
			sw = 1
		}
		subRes, err := evalDimensionResult(ctx, entries, sub, opts, sw)
		if err != nil {
			return res, err
		}
		subResults = append(subResults, subRes)
		if subRes.Skipped {
			continue
		}
		weighted += subRes.Score * sw
		totalWeight += sw
		if !subRes.Passed {
			anyFailed = true
		}
		if subRes.Score < minScore {
			minScore = subRes.Score
		}
	}

	res.SubResults = subResults
	agg := strings.ToLower(strings.TrimSpace(dim.Aggregation))

	switch agg {
	case "min":
		res.Score = minScore
		res.Detail = fmt.Sprintf("composite(min): min score=%.2f", minScore)
	case "cap_by_worst":
		if totalWeight > 0 {
			res.Score = weighted / totalWeight
		} else if len(subResults) > 0 {
			res.Score = 1
		}
		if anyFailed && minScore < res.Score {
			res.Score = minScore
		}
		res.Detail = fmt.Sprintf("composite(cap_by_worst): score=%.2f", res.Score)
	default: // weighted_sum
		if totalWeight > 0 {
			res.Score = weighted / totalWeight
		} else if len(subResults) > 0 {
			res.Score = 1
		}
		res.Detail = fmt.Sprintf("composite(weighted_sum): score=%.2f", res.Score)
	}

	res.Passed = res.Score >= dimensionPassScore(dim)
	if agg == "cap_by_worst" && anyFailed {
		res.Passed = false
	}
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

func dimensionPassScore(dim Dimension) float64 {
	if dim.PassScore > 0 {
		return dim.PassScore
	}
	return 1.0
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

func evalDimension(entries []tape.TapeEntry, dim Dimension) (float64, string, []AssertionResult, error) {
	if len(dim.Assertions) == 0 {
		if dim.Judge != nil {
			return 0, "judge pending", nil, nil
		}
		return 1, "no assertions", nil, nil
	}
	passed := 0
	var results []AssertionResult
	for _, a := range dim.Assertions {
		ok, detail, err := evalAssertion(entries, a)
		if err != nil {
			return 0, "", nil, err
		}
		if ok {
			passed++
		}
		results = append(results, buildAssertionResult(a, ok, detail, entries))
	}
	score := float64(passed) / float64(len(dim.Assertions))
	return score, fmt.Sprintf("%d/%d assertions passed", passed, len(dim.Assertions)), results, nil
}

func evalAssertion(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	if len(a.AnyOf) > 0 {
		for _, sub := range a.AnyOf {
			ok, detail, err := evalAssertion(entries, sub)
			if err != nil {
				return false, "", err
			}
			if ok {
				return !a.Negate, detail, nil
			}
		}
		return a.Negate, "", nil
	}

	fn, ok := assertionRegistry[a.Type]
	if !ok {
		return false, "", fmt.Errorf("unknown assertion type %q", a.Type)
	}
	result, detail, err := fn(entries, a)
	if err != nil {
		return false, "", err
	}
	if a.Negate {
		return !result, detail, nil
	}
	return result, detail, nil
}

func buildAssertionResult(a Assertion, ok bool, detail string, entries []tape.TapeEntry) AssertionResult {
	ar := AssertionResult{
		Type:    a.Type,
		Passed:  ok,
		Because: a.Because,
	}
	if detail != "" {
		parts := strings.SplitN(detail, "; actual=", 2)
		if len(parts) == 2 {
			ar.Expected = strings.TrimPrefix(parts[0], "expected=")
			ar.Actual = parts[1]
		} else {
			ar.Expected = detail
		}
	}
	if !ok {
		ar.Suggestion = suggestForAssertion(a, entries)
	}
	return ar
}

func suggestForAssertion(a Assertion, entries []tape.TapeEntry) string {
	switch a.Type {
	case "tool_called":
		if totalToolCalls(entries) == 0 {
			return "检查 prompt 是否清晰请求工具使用"
		}
		if sim := findSimilarToolName(entries, a.Name); sim != "" {
			return fmt.Sprintf("Did you mean: %s?", sim)
		}
	case "tool_not_called":
		if sim := findSimilarToolName(entries, a.Name); sim != "" {
			return fmt.Sprintf("Did you mean: %s?", sim)
		}
	case "tool_call_count":
		count := countToolCalls(entries, a.Name)
		if a.Max > 0 && count > a.Max {
			if a.Name == "shell" {
				return suggestShellBatching(entries, count, a.Max)
			}
			return fmt.Sprintf("reduce %s calls from %d to <= %d; consider batching", a.Name, count, a.Max)
		}
	case "tool_call_order":
		return fmt.Sprintf("expected order %v; actual order %v", a.Names, toolCallNames(entries))
	}
	return "run `dmr eval diagnose` for rule-based suggestions"
}

func suggestShellBatching(entries []tape.TapeEntry, count, max int) string {
	cmdCounts := make(map[string]int)
	for _, e := range entries {
		if e.Kind != "tool_call" {
			continue
		}
		calls, ok := tape.ExtractToolCalls(e.Payload)
		if !ok {
			continue
		}
		for _, c := range calls {
			if c.Name != "shell" {
				continue
			}
			args := map[string]any{}
			_ = json.Unmarshal([]byte(c.Arguments), &args)
			cmd, _ := args["cmd"].(string)
			prefix := shellCommandPrefix(cmd)
			cmdCounts[prefix]++
		}
	}
	var topPrefix string
	topN := 0
	for p, n := range cmdCounts {
		if n > topN {
			topN = n
			topPrefix = p
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "episode 包含 %d 次 shell 调用（超过阈值 %d）", count, max)
	if topPrefix != "" && topN > 1 {
		fmt.Fprintf(&sb, "；其中 %d 次是 `%s ...`，建议合并为批量命令或脚本", topN, topPrefix)
	}
	fmt.Fprint(&sb, "；用户拒绝后应避免立即重试")
	return sb.String()
}

func shellCommandPrefix(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	// Take first token or first two for "kubectl <verb>"
	fields := strings.Fields(cmd)
	if len(fields) >= 2 && fields[0] == "kubectl" {
		return "kubectl " + fields[1]
	}
	if len(fields) >= 1 {
		return fields[0]
	}
	return cmd
}

// Built-in assertions

func assertTaskStateFieldPresent(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	st := handoff.LatestState(entries)
	if st == nil {
		return false, "expected=task_state present; actual=no task_state entries", nil
	}
	switch a.Field {
	case "goal":
		if st.Goal != "" {
			return true, fmt.Sprintf("expected=goal present; actual=%s", st.Goal), nil
		}
		return false, "expected=goal present; actual=empty", nil
	default:
		return false, "", fmt.Errorf("unknown task_state field %q", a.Field)
	}
}

func assertTaskStateConstraint(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	st := handoff.LatestState(entries)
	if st == nil || st.Constraints == nil {
		return false, fmt.Sprintf("expected=constraint %s=%s; actual=no constraints", a.Key, a.Value), nil
	}
	v, ok := st.Constraints[a.Key]
	if !ok {
		return false, fmt.Sprintf("expected=constraint %s=%s; actual=key missing", a.Key, a.Value), nil
	}
	if v == a.Value {
		return true, fmt.Sprintf("expected=constraint %s=%s; actual=%s", a.Key, a.Value, v), nil
	}
	return false, fmt.Sprintf("expected=constraint %s=%s; actual=%s", a.Key, a.Value, v), nil
}

func assertToolCalled(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	count := countToolCalls(entries, a.Name)
	min := max1(a.Min)
	if count >= min {
		return true, fmt.Sprintf("expected=tool %s called >=%d; actual=%d", a.Name, min, count), nil
	}
	return false, fmt.Sprintf("expected=tool %s called >=%d; actual=%d", a.Name, min, count), nil
}

func assertToolNotCalled(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	count := countToolCalls(entries, a.Name)
	if count == 0 {
		return true, fmt.Sprintf("expected=tool %s not called; actual=0", a.Name), nil
	}
	return false, fmt.Sprintf("expected=tool %s not called; actual=%d", a.Name, count), nil
}

func assertTapeEntryKind(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	n := countKind(entries, a.Name)
	min := max1(a.Min)
	if n >= min {
		return true, fmt.Sprintf("expected=kind %s count >=%d; actual=%d", a.Name, min, n), nil
	}
	return false, fmt.Sprintf("expected=kind %s count >=%d; actual=%d", a.Name, min, n), nil
}

func assertAnchorExists(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	for _, e := range entries {
		if e.Kind != "anchor" {
			continue
		}
		name, _ := e.Payload["name"].(string)
		if name == a.Name {
			return true, fmt.Sprintf("expected=anchor %s; actual=found", a.Name), nil
		}
	}
	return false, fmt.Sprintf("expected=anchor %s; actual=missing", a.Name), nil
}

func assertLoopEvent(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
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
			return true, fmt.Sprintf("expected=event %s; actual=found", a.Name), nil
		}
		if matchEventField(data[a.Field], a.Value) {
			return true, fmt.Sprintf("expected=event %s %s=%s; actual=found", a.Name, a.Field, a.Value), nil
		}
	}
	return false, fmt.Sprintf("expected=event %s %s=%s; actual=missing", a.Name, a.Field, a.Value), nil
}

func assertTapeMustNotMatch(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	re, err := regexp.Compile(a.Regex)
	if err != nil {
		return false, "", err
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
			return false, fmt.Sprintf("expected=no match for %s; actual=matched", a.Regex), nil
		}
	}
	return true, fmt.Sprintf("expected=no match for %s; actual=no match", a.Regex), nil
}

func assertToolCallOrder(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	expected := a.Names
	if len(expected) == 0 {
		return false, "", fmt.Errorf("tool_call_order requires names")
	}
	actual := toolCallNames(entries)
	ok := subsequence(expected, actual)
	if a.Negate {
		// Negate handled by caller; here return raw result.
		return ok, fmt.Sprintf("expected=order %v; actual=%v", expected, actual), nil
	}
	if ok {
		return true, fmt.Sprintf("expected=order %v; actual=%v", expected, actual), nil
	}
	return false, fmt.Sprintf("expected=order %v; actual=%v", expected, actual), nil
}

func assertToolArgsMatch(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	if a.Name == "" {
		return false, "", fmt.Errorf("tool_args_match requires name")
	}
	calls := collectToolCalls(entries, a.Name)
	if len(calls) == 0 {
		return false, fmt.Sprintf("expected=%s args match; actual=not called", a.Name), nil
	}
	for _, c := range calls {
		args := map[string]any{}
		_ = json.Unmarshal([]byte(c.Arguments), &args)
		if a.Key == "" {
			if c.Arguments == a.Value {
				return true, fmt.Sprintf("expected=%s args=%s; actual=%s", a.Name, a.Value, c.Arguments), nil
			}
			continue
		}
		got, ok := args[a.Key]
		if !ok {
			continue
		}
		if fmt.Sprint(got) == a.Value {
			return true, fmt.Sprintf("expected=%s.%s=%s; actual=%v", a.Name, a.Key, a.Value, got), nil
		}
	}
	return false, fmt.Sprintf("expected=%s.%s=%s; actual=no matching call", a.Name, a.Key, a.Value), nil
}

func assertToolResultSuccess(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	var failures []string
	for _, e := range entries {
		if e.Kind != "tool_result" {
			continue
		}
		results, ok := tape.ExtractToolResults(e.Payload)
		if !ok {
			continue
		}
		for i, r := range results {
			if a.Name != "" {
				// Try to associate result with a preceding tool_call by index.
				callName := toolCallNameForResult(entries, e.ID, i)
				if callName != a.Name {
					continue
				}
			}
			if looksLikeError(r.Content) {
				failures = append(failures, r.Content)
			}
		}
	}
	if len(failures) == 0 {
		return true, "expected=all tool results succeed; actual=all succeed", nil
	}
	return false, fmt.Sprintf("expected=all tool results succeed; actual=%d failures", len(failures)), nil
}

func toolCallNameForResult(entries []tape.TapeEntry, resultID, resultIndex int) string {
	for _, e := range entries {
		if e.Kind != "tool_call" {
			continue
		}
		calls, ok := tape.ExtractToolCalls(e.Payload)
		if !ok {
			continue
		}
		for _, c := range calls {
			if c.ID == "" {
				continue
			}
			if c.ID == strconv.Itoa(resultID) {
				return c.Name
			}
		}
	}
	return ""
}

func assertToolCallCount(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	if a.Name == "" {
		return false, "", fmt.Errorf("tool_call_count requires name")
	}
	count := countToolCalls(entries, a.Name)
	min := a.Min
	max := a.Max
	if min < 0 && max <= 0 {
		min = 1
	}
	if count >= min && (max <= 0 || count <= max) {
		return true, fmt.Sprintf("expected=%s count in [%d,%d]; actual=%d", a.Name, min, max, count), nil
	}
	return false, fmt.Sprintf("expected=%s count in [%d,%d]; actual=%d", a.Name, min, max, count), nil
}

func assertEventOrder(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	expected := a.Names
	if len(expected) == 0 {
		return false, "", fmt.Errorf("event_order requires names")
	}
	actual := eventNames(entries)
	ok := subsequence(expected, actual)
	if ok {
		return true, fmt.Sprintf("expected=event order %v; actual=%v", expected, actual), nil
	}
	return false, fmt.Sprintf("expected=event order %v; actual=%v", expected, actual), nil
}

func eventNames(entries []tape.TapeEntry) []string {
	var names []string
	for _, e := range entries {
		if e.Kind != "event" {
			continue
		}
		name, _ := e.Payload["name"].(string)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func assertContextKeyPreserved(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	// Look at the last compact_summary and latest task_state for the key/value.
	var summary string
	for _, e := range entries {
		if e.Kind == "compact_summary" {
			content, _ := e.Payload["content"].(string)
			summary = content
		}
	}
	st := handoff.LatestState(entries)
	present := false
	if st != nil && st.Constraints != nil {
		if v, ok := st.Constraints[a.Key]; ok && v == a.Value {
			present = true
		}
	}
	if summary != "" && a.Key != "" {
		if strings.Contains(summary, a.Key) {
			present = true
		}
	}
	if present {
		return true, fmt.Sprintf("expected=%s=%s preserved; actual=preserved", a.Key, a.Value), nil
	}
	return false, fmt.Sprintf("expected=%s=%s preserved; actual=missing", a.Key, a.Value), nil
}

func assertStateConstraintPriority(entries []tape.TapeEntry, a Assertion) (bool, string, error) {
	expected := a.Names
	if len(expected) == 0 {
		return false, "", fmt.Errorf("state_constraint_priority requires names")
	}
	// Walk all task_state entries in tape order; for each expected key, the final
	// state's value should match the last value observed (latest instruction wins).
	latest := latestConstraintValues(entries)
	st := handoff.LatestState(entries)
	if st == nil || st.Constraints == nil {
		return false, fmt.Sprintf("expected=priority %v; actual=no constraints", expected), nil
	}
	for _, key := range expected {
		want, ok := latest[key]
		if !ok {
			return false, fmt.Sprintf("expected=priority key %s; actual=never set", key), nil
		}
		got, ok := st.Constraints[key]
		if !ok {
			return false, fmt.Sprintf("expected=priority key %s; actual=missing in final state", key), nil
		}
		if got != want {
			return false, fmt.Sprintf("expected=priority key %s=%s; actual=%s", key, want, got), nil
		}
	}
	return true, fmt.Sprintf("expected=priority %v; actual=final state matches latest values", expected), nil
}

func latestConstraintValues(entries []tape.TapeEntry) map[string]string {
	latest := map[string]string{}
	for _, e := range entries {
		if e.Kind != "task_state" {
			continue
		}
		st, err := handoff.StateFromPayload(e.Payload)
		if err != nil || st == nil || st.Constraints == nil {
			continue
		}
		for k, v := range st.Constraints {
			latest[k] = v
		}
	}
	return latest
}

func looksLikeError(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "error") || strings.Contains(s, "failed") || strings.Contains(s, "exception")
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

func collectToolCalls(entries []tape.TapeEntry, name string) []tape.ToolCallData {
	var out []tape.ToolCallData
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
				out = append(out, c)
			}
		}
	}
	return out
}

func toolCallNames(entries []tape.TapeEntry) []string {
	var names []string
	for _, e := range entries {
		if e.Kind != "tool_call" {
			continue
		}
		calls, ok := tape.ExtractToolCalls(e.Payload)
		if !ok {
			continue
		}
		for _, c := range calls {
			names = append(names, c.Name)
		}
	}
	return names
}

func totalToolCalls(entries []tape.TapeEntry) int {
	n := 0
	for _, e := range entries {
		if e.Kind != "tool_call" {
			continue
		}
		calls, ok := tape.ExtractToolCalls(e.Payload)
		if !ok {
			continue
		}
		n += len(calls)
	}
	return n
}

func findSimilarToolName(entries []tape.TapeEntry, target string) string {
	if target == "" {
		return ""
	}
	names := make(map[string]struct{})
	for _, n := range toolCallNames(entries) {
		names[n] = struct{}{}
	}
	var best string
	bestDist := math.MaxInt
	for n := range names {
		d := levenshtein(target, n)
		if d < bestDist && d <= 3 && d > 0 {
			bestDist = d
			best = n
		}
	}
	return best
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func subsequence(needle, haystack []string) bool {
	if len(needle) == 0 {
		return true
	}
	i := 0
	for _, h := range haystack {
		if h == needle[i] {
			i++
			if i == len(needle) {
				return true
			}
		}
	}
	return false
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
		fmt.Fprintf(&b, "  - %s: score=%.2f passed=%v%s %s", r.ID, r.Score, r.Passed, skip, r.Detail)
		if r.JudgeMeta != nil && r.JudgeMeta.Tokens > 0 {
			fmt.Fprintf(&b, " [tokens=%d cost=%.4f]", r.JudgeMeta.Tokens, r.JudgeMeta.Cost)
		}
		fmt.Fprintln(&b)
		for _, ar := range r.AssertionResults {
			if ar.Passed {
				continue
			}
			fmt.Fprintf(&b, "      assertion %s: expected=%q actual=%q", ar.Type, ar.Expected, ar.Actual)
			if ar.Because != "" {
				fmt.Fprintf(&b, " because=%q", ar.Because)
			}
			if ar.Suggestion != "" {
				fmt.Fprintf(&b, " suggestion=%q", ar.Suggestion)
			}
			fmt.Fprintln(&b)
		}
	}
	if card.Statistics != nil {
		s := card.Statistics
		fmt.Fprintf(&b, "  statistics: runs=%d success_rate=%.2f mean=%.2f stddev=%.2f min=%.2f max=%.2f\n",
			s.Runs, s.SuccessRate, s.Mean, s.StdDev, s.Min, s.Max)
	}
	return b.String()
}
