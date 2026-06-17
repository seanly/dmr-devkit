package handoff

import (
	"github.com/seanly/dmr-devkit/tape"
)

// StateUpdater updates structured task state from tape activity.
type StateUpdater interface {
	UpdateFromToolRound(prev *State, entries []tape.TapeEntry, step int, source string) State
	SnapshotForHandoff(prev *State, source string) State
	ResetForTopicSwitch(newGoal, source string) State
}
