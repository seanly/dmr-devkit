package eval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanly/dmr-devkit/tape"
)

func TestWriteTapeEntriesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.json")
	entries := []tape.TapeEntry{
			tape.NewMessageEntry(map[string]any{
			"role": "user", "content": "hello",
			}),
	}
	if err := WriteTapeEntries(path, entries); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTapeEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("len = %d", len(loaded))
	}
	_ = os.Remove(path)
}
