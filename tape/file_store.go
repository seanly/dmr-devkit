package tape

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileTapeStore persists tape entries as JSONL files under a directory.
type FileTapeStore struct {
	mu        sync.Mutex
	dir       string
	workspace string
	files     map[string]*os.File
	nextIDs   map[string]int
	tapeLocks map[string]*sync.Mutex
}

// NewFileTapeStore creates a FileTapeStore under dir.
func NewFileTapeStore(dir, workspace string) (*FileTapeStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create tape dir: %w", err)
	}
	s := &FileTapeStore{
		dir:       dir,
		workspace: workspace,
		files:     make(map[string]*os.File),
		nextIDs:   make(map[string]int),
		tapeLocks: make(map[string]*sync.Mutex),
	}
	return s, nil
}

func (s *FileTapeStore) tapePath(tape string) string {
	return filepath.Join(s.dir, tape+".jsonl")
}

func (s *FileTapeStore) nextIDPath(tape string) string {
	return filepath.Join(s.dir, tape+".nextid")
}

func (s *FileTapeStore) lockForTape(tape string) *sync.Mutex {
	s.mu.Lock()
	mu, ok := s.tapeLocks[tape]
	if !ok {
		mu = &sync.Mutex{}
		s.tapeLocks[tape] = mu
	}
	s.mu.Unlock()
	return mu
}

// Lock acquires an exclusive lock on the tape file. Call at startup to fail fast
// if another process is already using this session.
func (s *FileTapeStore) Lock(tape string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.getFile(tape)
	return err
}

func (s *FileTapeStore) getFile(tape string) (*os.File, error) {
	if f, ok := s.files[tape]; ok {
		return f, nil
	}

	path := s.tapePath(tape)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	// Acquire exclusive lock on the jsonl file itself
	if err := flockExclusive(int(f.Fd())); err != nil {
		f.Close()
		return nil, fmt.Errorf("session %q is already in use by another process", tape)
	}

	s.files[tape] = f

	// Read nextID from sidecar if present; otherwise fall back to counting lines
	nextID := 0
	if data, err := os.ReadFile(s.nextIDPath(tape)); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			nextID = n
		}
	} else if info, _ := f.Stat(); info.Size() > 0 {
		// Fallback for legacy tapes without sidecar
		_ = f.Sync()
		f.Seek(0, 0)
		scanner := bufio.NewScanner(f)
		count := 0
		for scanner.Scan() {
			count++
		}
		nextID = count
		// Restore seek position for append
		f.Seek(0, 2)
	}
	s.nextIDs[tape] = nextID

	return f, nil
}

func (s *FileTapeStore) ListTapes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, strings.TrimSuffix(e.Name(), ".jsonl"))
		}
	}
	return names
}

func (s *FileTapeStore) Reset(tape string) {
	mu := s.lockForTape(tape)
	mu.Lock()
	defer mu.Unlock()

	s.mu.Lock()
	if f, ok := s.files[tape]; ok {
		f.Close()
		delete(s.files, tape)
	}
	delete(s.nextIDs, tape)
	s.mu.Unlock()

	path := s.tapePath(tape)
	// Backup before reset (bub convention)
	if _, err := os.Stat(path); err == nil {
		bak := path + "." + time.Now().Format("20060102-150405") + ".bak"
		os.Rename(path, bak)
	}
	_ = os.Remove(s.nextIDPath(tape))
}

func (s *FileTapeStore) Append(tape string, entry TapeEntry) error {
	mu := s.lockForTape(tape)
	mu.Lock()
	defer mu.Unlock()

	s.mu.Lock()
	f, err := s.getFile(tape)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("open tape %q: %w", tape, err)
	}

	id := s.nextIDs[tape] + 1
	s.nextIDs[tape] = id
	entry.ID = id
	s.mu.Unlock()

	if entry.Meta == nil {
		entry.Meta = map[string]any{}
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write tape %q: %w", tape, err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write tape %q newline: %w", tape, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync tape %q: %w", tape, err)
	}
	// Persist nextID to sidecar
	_ = os.WriteFile(s.nextIDPath(tape), []byte(strconv.Itoa(id)+"\n"), 0o600)
	return nil
}

func (s *FileTapeStore) FetchAll(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	mu := s.lockForTape(tape)
	mu.Lock()

	// Flush any pending writes so the read sees all data
	s.mu.Lock()
	if f, ok := s.files[tape]; ok {
		if err := f.Sync(); err != nil {
			slog.Error("file store: sync failed before read", "tape", tape, "error", err)
		}
	}
	s.mu.Unlock()
	mu.Unlock()

	path := s.tapePath(tape)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			if opts == nil {
				return nil, nil
			}
			return applyFetchOpts(nil, opts)
		}
		return nil, err
	}
	defer f.Close()

	var entries []TapeEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	for scanner.Scan() {
		var e TapeEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			slog.Error("file store: skipping corrupted JSON line", "tape", tape, "error", err)
			continue
		}
		entries = append(entries, e)
	}

	if opts == nil {
		return entries, nil
	}
	return applyFetchOpts(entries, opts)
}

func applyFetchOpts(entries []TapeEntry, opts *FetchOpts) ([]TapeEntry, error) {
	if opts != nil && opts.AfterID > 0 {
		var filtered []TapeEntry
		for _, e := range entries {
			if e.ID > opts.AfterID {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
		o := *opts
		o.AfterID = 0
		o.LastAnchor = false
		o.AfterAnchor = ""
		o.BetweenAnchors = [2]string{}
		opts = &o
	}
	entries, err := applyAnchorSlicing(entries, opts)
	if err != nil {
		return nil, err
	}
	entries = applyDateFilter(entries, opts)
	entries = applyTextQuery(entries, opts)
	entries = applyKindFilter(entries, opts)
	if opts.Limit > 0 && len(entries) > opts.Limit {
		entries = entries[:opts.Limit]
	}
	return entries, nil
}

// Close closes all open file handles (flock released automatically on close).
func (s *FileTapeStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.files {
		f.Close()
	}
	s.files = make(map[string]*os.File)
}
