package eval

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/seanly/dmr-devkit/tape"
)

// WriteTapeEntries writes entries as {entries:[...]} JSON for fixture tapes.
func WriteTapeEntries(path string, entries []tape.TapeEntry) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	b, err := json.MarshalIndent(tapeFile{Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
