package agent

import (
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/core"
)

func TestCountSubagentDepth(t *testing.T) {
	cases := []struct {
		tape  string
		depth int
	}{
		{"main", 0},
		{"main:subagent:abc123", 1},
		{"main:subagent:abc123:subagent:def456", 2},
		{"main:subagent:abc:subagent:def:subagent:ghi", 3},
	}
	for _, c := range cases {
		got := countSubagentDepth(c.tape)
		if got != c.depth {
			t.Errorf("countSubagentDepth(%q) = %d, want %d", c.tape, got, c.depth)
		}
	}
}

func TestRunSubagentRejectsAtMaxDepth(t *testing.T) {
	a := New(nil, nil, nil, Config{})
	// Depth 3 should be rejected before any run logic.
	_, err := a.RunSubagent(nil, "main:subagent:a:subagent:b:subagent:c", "task", "", "temp", "", 0)
	if err == nil || !core.IsErrorKind(err, core.ErrDenied) || !strings.Contains(err.Error(), "max nesting depth 3 reached") {
		t.Fatalf("want depth error at depth 3, got %v", err)
	}
}
