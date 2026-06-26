package eval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/tape"
)

func TestLLMJudgeParsesJSON(t *testing.T) {
	chat := func(_ context.Context, _ string) (string, JudgeMeta, error) {
		return `{"score": 8, "reason": "good"}`, JudgeMeta{Tokens: 10}, nil
	}
	judge := LLMJudge(chat)
	score, detail, meta, err := judge(context.Background(), nil, JudgeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if score != 8 {
		t.Fatalf("expected score 8, got %f", score)
	}
	if detail != "good" {
		t.Fatalf("expected detail good, got %q", detail)
	}
	if meta.Tokens != 10 {
		t.Fatalf("expected tokens 10, got %d", meta.Tokens)
	}
}

func TestCachingJudge(t *testing.T) {
	calls := 0
	inner := func(_ context.Context, _ []tape.TapeEntry, _ JudgeSpec) (float64, string, JudgeMeta, error) {
		calls++
		return 7, "ok", JudgeMeta{Tokens: 5}, nil
	}
	judge := CachingJudge(inner)
	spec := JudgeSpec{Prompt: "test"}
	entries := []tape.TapeEntry{tape.NewMessageEntry(map[string]any{"role": "user", "content": "hi"})}

	score1, _, meta1, err := judge(context.Background(), entries, spec)
	if err != nil {
		t.Fatal(err)
	}
	score2, _, meta2, err := judge(context.Background(), entries, spec)
	if err != nil {
		t.Fatal(err)
	}
	if score1 != score2 {
		t.Fatalf("scores differ: %f vs %f", score1, score2)
	}
	if calls != 1 {
		t.Fatalf("expected 1 inner call, got %d", calls)
	}
	if meta1.CacheHit {
		t.Fatal("first call should not be cache hit")
	}
	if !meta2.CacheHit {
		t.Fatal("second call should be cache hit")
	}
}

func TestEnsembleJudge(t *testing.T) {
	j1 := func(_ context.Context, _ []tape.TapeEntry, _ JudgeSpec) (float64, string, JudgeMeta, error) {
		return 6, "j1", JudgeMeta{Tokens: 1}, nil
	}
	j2 := func(_ context.Context, _ []tape.TapeEntry, _ JudgeSpec) (float64, string, JudgeMeta, error) {
		return 10, "j2", JudgeMeta{Tokens: 1}, nil
	}
	judge := EnsembleJudge([]JudgeFunc{j1, j2})
	score, detail, meta, err := judge(context.Background(), nil, JudgeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if score != 8 {
		t.Fatalf("expected ensemble score 8, got %f", score)
	}
	if meta.Tokens != 2 {
		t.Fatalf("expected tokens 2, got %d", meta.Tokens)
	}
	if !contains(detail, "judge0=6.00") || !contains(detail, "judge1=10.00") {
		t.Fatalf("unexpected detail: %s", detail)
	}
}

func TestEnsembleJudgePropagatesError(t *testing.T) {
	j1 := func(_ context.Context, _ []tape.TapeEntry, _ JudgeSpec) (float64, string, JudgeMeta, error) {
		return 0, "", JudgeMeta{}, errors.New("boom")
	}
	judge := EnsembleJudge([]JudgeFunc{j1})
	_, _, _, err := judge(context.Background(), nil, JudgeSpec{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestJudgeMetaLatency(t *testing.T) {
	chat := func(_ context.Context, _ string) (string, JudgeMeta, error) {
		time.Sleep(5 * time.Millisecond)
		return "7", JudgeMeta{}, nil
	}
	judge := LLMJudge(chat)
	_, _, meta, err := judge(context.Background(), nil, JudgeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Latency < 5*time.Millisecond {
		t.Fatalf("expected latency >= 5ms, got %s", meta.Latency)
	}
}
