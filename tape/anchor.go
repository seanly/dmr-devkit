package tape

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/seanly/dmr-devkit/core"
)

const (
	// StateKeyAnchorUUID is stored in anchor payload state for handoff checkpoints.
	StateKeyAnchorUUID = "anchor_uuid"
	// StateKeyTapeUUID identifies a subtape in session/start anchor state.
	StateKeyTapeUUID = "tape_uuid"
)

// NewUUID returns a new random UUID string (v4).
func NewUUID() string {
	return uuid.NewString()
}

// AnchorState returns the state map from an anchor entry payload.
func AnchorState(e TapeEntry) map[string]any {
	if e.Kind != "anchor" {
		return nil
	}
	st, _ := e.Payload["state"].(map[string]any)
	return st
}

// AnchorUUIDFromEntry reads anchor_uuid from an anchor entry's state.
func AnchorUUIDFromEntry(e TapeEntry) string {
	st := AnchorState(e)
	if st == nil {
		return ""
	}
	u, _ := st[StateKeyAnchorUUID].(string)
	return strings.TrimSpace(u)
}

// TapeUUIDFromSessionStart reads tape_uuid from the session/start anchor on a tape.
func TapeUUIDFromSessionStart(store TapeStore, tapeName string) (string, error) {
	entries, err := store.FetchAll(tapeName, &FetchOpts{Kinds: []string{"anchor"}})
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name, _ := e.Payload["name"].(string)
		if name != "session/start" {
			continue
		}
		st := AnchorState(e)
		if st == nil {
			continue
		}
		if u, _ := st[StateKeyTapeUUID].(string); strings.TrimSpace(u) != "" {
			return strings.TrimSpace(u), nil
		}
	}
	return "", nil
}

func matchUUIDPrefix(full, prefix string) bool {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	full = strings.TrimSpace(strings.ToLower(full))
	if prefix == "" || full == "" {
		return false
	}
	return strings.HasPrefix(full, prefix)
}

// FindAnchorByUUID locates an anchor on tapeName whose anchor_uuid matches prefix.
// prefix may be a full UUID or a unique leading substring (e.g. 8 hex chars).
func FindAnchorByUUID(store TapeStore, tapeName, prefix string) (TapeEntry, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return TapeEntry{}, fmt.Errorf("anchor uuid required")
	}
	entries, err := store.FetchAll(tapeName, &FetchOpts{Kinds: []string{"anchor"}})
	if err != nil {
		return TapeEntry{}, err
	}
	var matches []TapeEntry
	for _, e := range entries {
		u := AnchorUUIDFromEntry(e)
		if u != "" && matchUUIDPrefix(u, prefix) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return TapeEntry{}, core.NewError(core.ErrNotFound, fmt.Sprintf("anchor uuid %q not found on tape %q", prefix, tapeName), nil)
	case 1:
		return matches[0], nil
	default:
		return TapeEntry{}, fmt.Errorf("ambiguous anchor uuid prefix %q: %d matches on tape %q", prefix, len(matches), tapeName)
	}
}

// FindSubtapeByTapeUUID scans tapes under namespace (tape names equal to namespace
// or prefixed with namespace+":") for session/start.state.tape_uuid matching prefix.
func FindSubtapeByTapeUUID(store TapeStore, namespace, prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", fmt.Errorf("tape uuid required")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "", fmt.Errorf("namespace required")
	}
	var matches []string
	for _, name := range store.ListTapes() {
		if name != namespace && !strings.HasPrefix(name, namespace+":") {
			continue
		}
		if name == namespace {
			continue // root tape has no tape_uuid for delete
		}
		u, err := TapeUUIDFromSessionStart(store, name)
		if err != nil || u == "" {
			continue
		}
		if matchUUIDPrefix(u, prefix) {
			matches = append(matches, name)
		}
	}
	switch len(matches) {
	case 0:
		return "", core.NewError(core.ErrNotFound, fmt.Sprintf("subtape with tape_uuid %q not found under %q", prefix, namespace), nil)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous tape uuid prefix %q: %d subtapes under %q", prefix, len(matches), namespace)
	}
}

// AnchorInfo summarizes one anchor for listing.
type AnchorInfo struct {
	Name       string
	EntryID    int
	Date       string
	AnchorUUID string
}

// ListAnchorsWithUUID returns anchors on tapeName in tape order with UUIDs when present.
func ListAnchorsWithUUID(store TapeStore, tapeName string) ([]AnchorInfo, error) {
	entries, err := store.FetchAll(tapeName, &FetchOpts{Kinds: []string{"anchor"}})
	if err != nil {
		return nil, err
	}
	out := make([]AnchorInfo, 0, len(entries))
	for _, e := range entries {
		name, _ := e.Payload["name"].(string)
		out = append(out, AnchorInfo{
			Name:       name,
			EntryID:    e.ID,
			Date:       e.Date,
			AnchorUUID: AnchorUUIDFromEntry(e),
		})
	}
	return out, nil
}
