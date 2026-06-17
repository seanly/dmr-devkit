package eval

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/seanly/dmr-devkit/tape"
)

type tapeFile struct {
	Entries []tape.TapeEntry `json:"entries"`
}

// LoadTapeEntries reads tape entries from a JSON file ({entries:[...]} or raw array).
func LoadTapeEntries(path string) ([]tape.TapeEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wrapped tapeFile
	if err := json.Unmarshal(b, &wrapped); err == nil && len(wrapped.Entries) > 0 {
		return wrapped.Entries, nil
	}
	var entries []tape.TapeEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("parse tape JSON: %w", err)
	}
	return entries, nil
}
