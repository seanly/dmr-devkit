package agent

import (
	"testing"

	"github.com/seanly/dmr-devkit/handoff"
)

func TestValidateCompactSummary(t *testing.T) {
	st := &handoff.State{Goal: "refactor handoff pipeline"}
	if !validateCompactSummary(st, "We refactored the handoff pipeline successfully") {
		t.Fatal("expected pass when summary contains goal token")
	}
	if validateCompactSummary(st, "unrelated summary without keywords") {
		t.Fatal("expected fail for unrelated summary")
	}
	if !validateCompactSummary(nil, "any summary") {
		t.Fatal("expected pass when no state but summary present")
	}
}
