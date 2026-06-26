package eval

import "testing"

func TestTextReporter(t *testing.T) {
	r := TextReporter{}
	cards := []*ScoreCard{
		{Rubric: "a", Passed: true, Score: 1.0, PassScore: 0.8},
		{Rubric: "b", Passed: false, Score: 0.5, PassScore: 0.8},
	}
	data, err := r.Report(1, 2, cards)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !contains(out, "1/2 fixtures pass") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestJSONReporter(t *testing.T) {
	r := JSONReporter{}
	cards := []*ScoreCard{
		{Rubric: "a", Passed: true, Score: 1.0, PassScore: 0.8},
	}
	data, err := r.Report(1, 1, cards)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !contains(out, `"passed": 1`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestMarkdownReporter(t *testing.T) {
	r := MarkdownReporter{}
	cards := []*ScoreCard{
		{Rubric: "a", Passed: true, Score: 1.0, PassScore: 0.8},
	}
	data, err := r.Report(1, 1, cards)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !contains(out, "# Eval Report") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestJUnitReporter(t *testing.T) {
	r := JUnitReporter{}
	cards := []*ScoreCard{
		{Rubric: "a", Passed: false, Score: 0.5, PassScore: 0.8},
	}
	data, err := r.Report(0, 1, cards)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !contains(out, "testsuites") {
		t.Fatalf("unexpected output: %s", out)
	}
	if !contains(out, `failures="1"`) {
		t.Fatalf("expected failure count: %s", out)
	}
}
