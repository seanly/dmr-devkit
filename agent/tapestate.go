package agent

import (
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/tool"
)

// tapeState holds all mutable runtime state for a single tape.
// Each tape has its own mutex for field access; tapeStateMap's mutex protects
// the map itself and coordinates LRU eviction. This eliminates the previous
// split-lock design that caused deadlocks and lock-order inversions.
type tapeState struct {
	mu              sync.Mutex
	chatClient      *client.ChatClient
	sessionStarted  bool
	modelOverride   string
	lastCompactStep int
	discoveredTools map[string]bool // toolName -> discovered
	toolsCache      []*tool.Tool    // cached eligible tools for this tape
	lastAccessed    int64           // unix nanos for LRU eviction
	budget          *contextBudget
}

func newTapeState() *tapeState {
	return &tapeState{
		discoveredTools: make(map[string]bool),
		toolsCache:      nil,
		lastAccessed:    time.Now().UnixNano(),
		budget:          newContextBudget(),
		lastCompactStep: -1,
	}
}

// touch updates the last-accessed timestamp for LRU eviction.
func (ts *tapeState) touch() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.lastAccessed = time.Now().UnixNano()
}

// tapeStateMap holds per-tape runtime state with coherent locking.
type tapeStateMap struct {
	mu     sync.RWMutex
	states map[string]*tapeState
	max    int
}

func newTapeStateMap(max int) tapeStateMap {
	return tapeStateMap{
		states: make(map[string]*tapeState),
		max:    max,
	}
}

// get returns the tape state if it exists (read lock).
func (m *tapeStateMap) get(tapeName string) *tapeState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[tapeName]
}

// getOrCreate returns the existing tape state or creates one under the write lock.
// It also updates lastAccessed. Eviction (LRU) happens only on create.
func (m *tapeStateMap) getOrCreate(tapeName string) *tapeState {
	m.mu.RLock()
	if ts, ok := m.states[tapeName]; ok {
		ts.touch()
		m.mu.RUnlock()
		return ts
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if ts, ok := m.states[tapeName]; ok {
		ts.touch()
		return ts
	}

	if m.max > 0 && len(m.states) >= m.max {
		m.evictLRULocked()
	}

	ts := newTapeState()
	m.states[tapeName] = ts
	return ts
}

// remove deletes a tape state (used by subagent cleanup).
func (m *tapeStateMap) remove(tapeName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, tapeName)
}

// evictLRULocked removes the least-recently-accessed tape state.
// Caller must hold m.mu write lock.
func (m *tapeStateMap) evictLRULocked() {
	if len(m.states) == 0 {
		return
	}
	var oldest string
	var oldestTime int64
	for name, ts := range m.states {
		ts.mu.Lock()
		accessed := ts.lastAccessed
		ts.mu.Unlock()
		if oldest == "" || accessed < oldestTime {
			oldest = name
			oldestTime = accessed
		}
	}
	if oldest != "" {
		delete(m.states, oldest)
	}
}

// rangeLocked calls fn for every state while holding the write lock.
// Useful for operations that must visit all tapes atomically.
func (m *tapeStateMap) rangeLocked(fn func(name string, ts *tapeState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, ts := range m.states {
		fn(name, ts)
	}
}
