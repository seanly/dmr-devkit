package eval

import (
	"fmt"
	"strings"
)

// Reporter formats the outcome of a baseline run.
type Reporter interface {
	Report(passed, total int, cards []*ScoreCard) ([]byte, error)
}

// TextReporter produces the canonical human-readable baseline report.
type TextReporter struct{}

func (TextReporter) Report(passed, total int, cards []*ScoreCard) ([]byte, error) {
	return []byte(FormatBaselineReport(passed, total, cards)), nil
}

// JSONReporter produces a structured JSON baseline report.
type JSONReporter struct{}

func (JSONReporter) Report(passed, total int, cards []*ScoreCard) ([]byte, error) {
	return FormatBaselineReportJSON(passed, total, cards)
}

// MarkdownReporter produces a GitHub-friendly Markdown summary.
type MarkdownReporter struct{}

func (MarkdownReporter) Report(passed, total int, cards []*ScoreCard) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Eval Report\n\n")
	fmt.Fprintf(&b, "- **Fixtures**: %d / %d passed\n", passed, total)
	passRate := 0.0
	if total > 0 {
		passRate = float64(passed) / float64(total)
	}
	fmt.Fprintf(&b, "- **Pass Rate**: %.1f%%\n", passRate*100)
	fmt.Fprintf(&b, "\n| Status | Fixture | Score | Threshold |\n")
	fmt.Fprintf(&b, "|--------|---------|-------|-----------|\n")
	for _, c := range cards {
		if c == nil {
			continue
		}
		status := "❌ FAIL"
		if c.Passed {
			status = "✅ PASS"
		}
		fmt.Fprintf(&b, "| %s | %s | %.2f | %.2f |\n", status, c.Rubric, c.Score, c.PassScore)
	}
	return []byte(b.String()), nil
}

// JUnitReporter produces a JUnit XML baseline report.
type JUnitReporter struct{}

func (JUnitReporter) Report(passed, total int, cards []*ScoreCard) ([]byte, error) {
	// Reuse the dmr/pkg/evalreport logic when available; here we provide a minimal fallback.
	var b strings.Builder
	fmt.Fprintf(&b, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(&b, "<testsuites name=\"dmr-eval\" tests=\"%d\" failures=\"%d\">\n", total, total-passed)
	for _, c := range cards {
		if c == nil {
			continue
		}
		fmt.Fprintf(&b, "  <testsuite name=\"%s\" tests=\"1\" failures=\"%d\">\n", c.Rubric, boolToInt(!c.Passed))
		fmt.Fprintf(&b, "    <testcase name=\"%s\" classname=\"dmr.eval.fixture\"/>\n", c.Rubric)
		if !c.Passed {
			fmt.Fprintf(&b, "    <failure message=\"score %.2f below threshold %.2f\" type=\"AssertionFailure\"/>\n", c.Score, c.PassScore)
		}
		fmt.Fprintf(&b, "  </testsuite>\n")
	}
	fmt.Fprintf(&b, "</testsuites>\n")
	return []byte(b.String()), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
