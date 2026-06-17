package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RunBaselineFixtures runs all bundled fixtures under dir and returns pass count.
func RunBaselineFixtures(dir string) (passed, total int, cards []*ScoreCard, err error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return 0, 0, nil, err
	}
	for _, m := range matches {
		card, runErr := RunFixture(m)
		if runErr != nil {
			return passed, total, cards, fmt.Errorf("%s: %w", m, runErr)
		}
		cards = append(cards, card)
		total++
		if card.Passed {
			passed++
		}
	}
	return passed, total, cards, nil
}

// FormatBaselineReport summarizes baseline fixture results.
func FormatBaselineReport(passed, total int, cards []*ScoreCard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "eval baseline: %d/%d fixtures pass\n", passed, total)
	for _, c := range cards {
		if c == nil {
			continue
		}
		status := "FAIL"
		if c.Passed {
			status = "PASS"
		}
		fmt.Fprintf(&b, "  %s %s score=%.2f\n", status, c.Rubric, c.Score)
	}
	return b.String()
}

// WriteBaselineStamp writes a one-line stamp for doctor hints.
func WriteBaselineStamp(path string, passed, total int) error {
	if path == "" {
		return nil
	}
	line := fmt.Sprintf("%d/%d %s\n", passed, total, strings.TrimSpace(os.Getenv("USER")))
	return os.WriteFile(path, []byte(line), 0o644)
}
