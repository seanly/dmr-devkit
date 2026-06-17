package agent

import (
	"testing"

	devkitcfg "github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/tape"
)

func TestInitTaskStateFromPrompt(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	enabled := true
	a := New(nil, tm, nil, Config{
		AgentPolicy: devkitcfg.AgentConfig{
			Handoff: devkitcfg.HandoffConfig{StateEnabled: &enabled},
		},
	})

	const tapeName = "main"
	a.initTaskStateFromPrompt(tapeName, "Book Paris tickets, window seat")

	st := a.latestTaskState(tapeName)
	if st == nil {
		t.Fatal("expected task state")
	}
	if st.Goal != "Book Paris tickets, window seat" {
		t.Fatalf("goal = %q", st.Goal)
	}
	if st.Source != "heuristic" {
		t.Fatalf("source = %q", st.Source)
	}

	// Second call must not overwrite initial state.
	a.initTaskStateFromPrompt(tapeName, "different goal")
	st2 := a.latestTaskState(tapeName)
	if st2.Goal != st.Goal {
		t.Fatalf("goal overwritten: %q", st2.Goal)
	}
}

func TestSnapshotTaskStateBeforeHandoff(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	enabled := true
	a := New(nil, tm, nil, Config{
		AgentPolicy: devkitcfg.AgentConfig{
			Handoff: devkitcfg.HandoffConfig{StateEnabled: &enabled},
		},
	})

	const tapeName = "handoff-test"
	a.initTaskStateFromPrompt(tapeName, "Ship harness improvements")

	id := a.snapshotTaskStateBeforeHandoff(tapeName, 2)
	if id == 0 {
		t.Fatal("expected task_state entry id")
	}

	st := a.latestTaskState(tapeName)
	if st == nil || st.Source != "handoff" {
		t.Fatalf("latest state = %+v", st)
	}
	if st.Goal != "Ship harness improvements" {
		t.Fatalf("goal = %q", st.Goal)
	}

	entries, err := a.fetchTapeEntries(tapeName)
	if err != nil {
		t.Fatal(err)
	}
	nState := 0
	for _, e := range entries {
		if e.Kind == "task_state" {
			nState++
		}
	}
	if nState != 2 {
		t.Fatalf("expected 2 task_state entries, got %d", nState)
	}
}

func TestTaskStateDisabled(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	disabled := false
	a := New(nil, tm, nil, Config{
		AgentPolicy: devkitcfg.AgentConfig{
			Handoff: devkitcfg.HandoffConfig{
				StateEnabled: &disabled,
			},
		},
	})

	a.initTaskStateFromPrompt("t", "goal")
	if a.latestTaskState("t") != nil {
		t.Fatal("task state should be disabled")
	}
}
