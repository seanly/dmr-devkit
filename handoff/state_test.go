package handoff

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tape"
)

func TestStateValidate(t *testing.T) {
	s := NewState("fix bug", "heuristic")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLatestState(t *testing.T) {
	entries := []tape.TapeEntry{
		tape.NewTaskStateEntry(NewState("first", "heuristic").ToPayload()),
		tape.NewTaskStateEntry(NewState("second", "handoff").ToPayload()),
	}
	got := LatestState(entries)
	if got == nil || got.Goal != "second" {
		t.Fatalf("goal = %v", got)
	}
}

func TestHeuristicUpdaterToolRound(t *testing.T) {
	u := NewUpdater(DefaultUpdaterOptions())
	entries := []tape.TapeEntry{
		tape.NewMessageEntry(map[string]any{"role": "user", "content": "read loop.go"}),
		tape.NewToolCallEntry([]map[string]any{{
			"id": "1", "type": "function",
			"function": map[string]any{"name": "fsRead", "arguments": `{"path":"agent/loop.go"}`},
		}}),
	}
	s := u.UpdateFromToolRound(nil, entries, 1, "heuristic")
	if s.Goal != "read loop.go" {
		t.Fatalf("goal = %q", s.Goal)
	}
	if !containsArtifact(s.Artifacts, "agent/loop.go") && !containsArtifact(s.Artifacts, "/loop.go") && !containsArtifactPrefix(s.Artifacts, "loop.go") {
		t.Fatalf("artifacts = %v", s.Artifacts)
	}
}

func containsArtifactPrefix(arts []Artifact, suffix string) bool {
	for _, a := range arts {
		if strings.HasSuffix(a.Ref, suffix) {
			return true
		}
	}
	return false
}

func containsArtifact(arts []Artifact, ref string) bool {
	for _, a := range arts {
		if a.Ref == ref {
			return true
		}
	}
	return false
}

func TestFormatPromptBlock(t *testing.T) {
	s := NewState("ship feature", "handoff")
	s.Constraints = map[string]string{"scope": "session"}
	block := s.FormatPromptBlock()
	if !contains(block, "TaskState v1") || !contains(block, "ship feature") {
		t.Fatalf("block = %q", block)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
