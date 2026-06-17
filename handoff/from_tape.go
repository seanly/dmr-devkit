package handoff

import (
	"github.com/seanly/dmr-devkit/tape"
)

// LatestState returns the most recent task_state from entries (in tape order).
func LatestState(entries []tape.TapeEntry) *State {
	var latest *State
	for _, e := range entries {
		if e.Kind != "task_state" {
			continue
		}
		s, err := StateFromPayload(e.Payload)
		if err != nil {
			continue
		}
		latest = s
	}
	return latest
}

// LatestStateFromTape reads all entries for a tape and returns latest task_state.
func LatestStateFromTape(store tape.TapeStore, tapeName string) (*State, error) {
	entries, err := store.FetchAll(tapeName, nil)
	if err != nil {
		return nil, err
	}
	return LatestState(entries), nil
}
