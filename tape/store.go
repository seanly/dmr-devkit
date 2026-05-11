package tape

import (
	"strings"
	"sync"

	"github.com/seanly/dmr-devkit/core"
)

// TapeStore is the interface for tape persistence.
type TapeStore interface {
	ListTapes() []string
	Reset(tape string)
	FetchAll(tape string, opts *FetchOpts) ([]TapeEntry, error)
	Append(tape string, entry TapeEntry) error
}

// FetchOpts controls filtering when fetching entries.
type FetchOpts struct {
	AfterAnchor    string
	LastAnchor     bool
	BetweenAnchors [2]string // [start, end]
	// AfterID, if > 0, keeps only entries with ID strictly greater than AfterID (tape row id).
	// When set, anchor-based fields above are ignored by stores that branch on FetchAll.
	StartDate string
	EndDate   string
	TextQuery string
	Kinds     []string
	Limit     int
	AfterID   int
}

// InMemoryTapeStore implements TapeStore using an in-memory map.
type InMemoryTapeStore struct {
	mu      sync.RWMutex
	tapes   map[string][]TapeEntry
	nextIDs map[string]int
}

func NewInMemoryTapeStore() *InMemoryTapeStore {
	return &InMemoryTapeStore{
		tapes:   make(map[string][]TapeEntry),
		nextIDs: make(map[string]int),
	}
}

func (s *InMemoryTapeStore) ListTapes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.tapes))
	for name := range s.tapes {
		names = append(names, name)
	}
	return names
}

func (s *InMemoryTapeStore) Reset(tape string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tapes, tape)
	delete(s.nextIDs, tape)
}

func (s *InMemoryTapeStore) Append(tape string, entry TapeEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextIDs[tape]
	s.nextIDs[tape] = id + 1
	entry.ID = id
	s.tapes[tape] = append(s.tapes[tape], entry)
	return nil
}

func (s *InMemoryTapeStore) FetchAll(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	s.mu.RLock()
	entries := make([]TapeEntry, len(s.tapes[tape]))
	copy(entries, s.tapes[tape])
	s.mu.RUnlock()

	if opts == nil {
		return entries, nil
	}

	return applyFetchOpts(entries, opts)
}

func applyAnchorSlicing(entries []TapeEntry, opts *FetchOpts) ([]TapeEntry, error) {
	if opts.LastAnchor {
		lastIdx := -1
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].Kind == "anchor" {
				lastIdx = i
				break
			}
		}
		if lastIdx == -1 {
			return nil, core.NewError(core.ErrNotFound, "no anchor found in tape", nil)
		}
		return entries[lastIdx+1:], nil
	}

	if opts.AfterAnchor != "" {
		idx := -1
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].Kind == "anchor" {
				name, _ := entries[i].Payload["name"].(string)
				if name == opts.AfterAnchor {
					idx = i
					break
				}
			}
		}
		if idx == -1 {
			return nil, core.NewError(core.ErrNotFound, "anchor '"+opts.AfterAnchor+"' not found", nil)
		}
		return entries[idx+1:], nil
	}

	if opts.BetweenAnchors[0] != "" && opts.BetweenAnchors[1] != "" {
		startIdx := -1
		endIdx := -1
		for i, e := range entries {
			if e.Kind == "anchor" {
				name, _ := e.Payload["name"].(string)
				if name == opts.BetweenAnchors[0] {
					startIdx = i
				}
				if name == opts.BetweenAnchors[1] {
					endIdx = i
				}
			}
		}
		if startIdx == -1 {
			return nil, core.NewError(core.ErrNotFound, "anchor '"+opts.BetweenAnchors[0]+"' not found", nil)
		}
		if endIdx == -1 {
			return nil, core.NewError(core.ErrNotFound, "anchor '"+opts.BetweenAnchors[1]+"' not found", nil)
		}
		if startIdx >= endIdx {
			return []TapeEntry{}, nil
		}
		return entries[startIdx+1 : endIdx], nil
	}

	return entries, nil
}

func applyDateFilter(entries []TapeEntry, opts *FetchOpts) []TapeEntry {
	if opts.StartDate == "" && opts.EndDate == "" {
		return entries
	}
	startCmp := opts.StartDate
	endCmp := normEndDate(opts.EndDate)
	var filtered []TapeEntry
	for _, e := range entries {
		if e.Date == "" {
			continue
		}
		if startCmp != "" && e.Date < startCmp {
			continue
		}
		if endCmp != "" && e.Date > endCmp {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// normEndDate appends T23:59:59 when the value is a bare YYYY-MM-DD date
// so that the comparison includes the entire day. If the value already
// contains a time component it is returned as-is.
func normEndDate(v string) string {
	if v != "" && len(v) <= 10 {
		return v + "T23:59:59"
	}
	return v
}

func applyTextQuery(entries []TapeEntry, opts *FetchOpts) []TapeEntry {
	if opts.TextQuery == "" {
		return entries
	}
	q := strings.ToLower(opts.TextQuery)
	var filtered []TapeEntry
	for _, e := range entries {
		if matchText(e.Payload, q) || matchText(e.Meta, q) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func matchText(m map[string]any, query string) bool {
	for _, v := range m {
		switch val := v.(type) {
		case string:
			if strings.Contains(strings.ToLower(val), query) {
				return true
			}
		case map[string]any:
			if matchText(val, query) {
				return true
			}
		}
	}
	return false
}

func applyKindFilter(entries []TapeEntry, opts *FetchOpts) []TapeEntry {
	if len(opts.Kinds) == 0 {
		return entries
	}
	kindSet := make(map[string]bool, len(opts.Kinds))
	for _, k := range opts.Kinds {
		kindSet[k] = true
	}
	var filtered []TapeEntry
	for _, e := range entries {
		if kindSet[e.Kind] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
