package tape

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestFileStoreConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileTapeStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tape := string(rune('A' + n%2)) // 2 tapes: "A" and "B"
			if err := s.Append(tape, TapeEntry{Kind: "message", Payload: map[string]any{"n": n}}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	for _, tape := range []string{"A", "B"} {
		entries, err := s.FetchAll(tape, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Fatalf("expected entries for tape %s", tape)
		}
	}
}

func TestFileStoreNextIDSidecar(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileTapeStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append("x", TapeEntry{Kind: "msg", Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("x", TapeEntry{Kind: "msg", Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := NewFileTapeStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := s2.Append("x", TapeEntry{Kind: "msg", Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	entries, err := s2.FetchAll("x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	for i, e := range entries {
		if e.ID != i+1 {
			t.Fatalf("expected ID %d, got %d", i+1, e.ID)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "x.nextid")); err != nil {
		t.Fatal("expected sidecar file to exist")
	}
}

func TestFileStore_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission tests skipped on Windows")
	}

	dir := t.TempDir()
	s, err := NewFileTapeStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Append("permtest", TapeEntry{Kind: "msg", Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}

	// Check tape file permission
	tapePath := filepath.Join(dir, "permtest.jsonl")
	info, err := os.Stat(tapePath)
	if err != nil {
		t.Fatalf("expected tape file to exist: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected tape file permission 0o600, got 0o%03o", perm)
	}

	// Check nextid sidecar permission
	sidecarPath := filepath.Join(dir, "permtest.nextid")
	info, err = os.Stat(sidecarPath)
	if err != nil {
		t.Fatalf("expected sidecar file to exist: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected sidecar file permission 0o600, got 0o%03o", perm)
	}
}

func TestFileStore_FetchAllSkipsCorruptedLines(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileTapeStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Append two valid entries
	for i := 0; i < 2; i++ {
		if err := s.Append("corrupt", TapeEntry{Kind: "msg", Payload: map[string]any{"i": i}}); err != nil {
			t.Fatal(err)
		}
	}

	// Directly inject a corrupted line into the file
	tapePath := filepath.Join(dir, "corrupt.jsonl")
	f, err := os.OpenFile(tapePath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("this is not json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Append another valid entry after the corruption
	if err := s.Append("corrupt", TapeEntry{Kind: "msg", Payload: map[string]any{"i": 2}}); err != nil {
		t.Fatal(err)
	}

	entries, err := s.FetchAll("corrupt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 valid entries (corrupted line skipped), got %d", len(entries))
	}
	for i, e := range entries {
		if e.ID != i+1 {
			t.Errorf("expected ID %d at position %d, got %d", i+1, i, e.ID)
		}
	}
}
