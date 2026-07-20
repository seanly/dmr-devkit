package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/seanly/dmr-devkit/tape"
)

// readToolNames is the set of tool names whose arguments are treated as file
// reads when reconstructing recently-accessed files for post-compact rebuild.
var readToolNames = map[string]bool{
	"read": true, "read_file": true, "file_read": true, "view_file": true,
	"view": true, "cat": true, "get_file": true, "open_file": true,
}

const (
	maxRecentFiles      = 5
	maxArchivedToolHint = 5
)

// compactAnchorMeta captures anchor boundary metadata written during compact.
type compactAnchorMeta struct {
	AnchorName            string
	AnchorUUID            string
	AnchorEntryID         int
	PreviousAnchorName    string
	PreviousAnchorUUID    string
	PreviousAnchorEntryID int
	TriggerReason         string
}

func compactAnchorMetaFromEntry(e tape.TapeEntry) compactAnchorMeta {
	if e.Kind != "anchor" {
		return compactAnchorMeta{}
	}
	st := tape.AnchorState(e)
	if st == nil {
		return compactAnchorMeta{}
	}
	meta := compactAnchorMeta{
		AnchorName:         stringFromAny(e.Payload["name"]),
		AnchorUUID:         stringFromAny(st[tape.StateKeyAnchorUUID]),
		AnchorEntryID:      e.ID,
		PreviousAnchorName: stringFromAny(st["previous_anchor_name"]),
		PreviousAnchorUUID: stringFromAny(st["previous_anchor_uuid"]),
		TriggerReason:      stringFromAny(st["trigger_reason"]),
	}
	if id, ok := intFromAny(st["previous_anchor_entry_id"]); ok {
		meta.PreviousAnchorEntryID = id
	}
	return meta
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// rebuildPostCompactContext re-injects a minimal working environment after a
// compact so the model does not start from an empty context. It appends a
// single system entry to the tape summarizing:
//   - archived window boundaries and tapeSearch retrieval hints
//   - recently accessed file paths (extracted from tool_call entries)
//   - the names of tools already discovered in this session
func (a *Agent) rebuildPostCompactContext(ctx context.Context, tapeName string, meta compactAnchorMeta) {
	if a.tape == nil || a.tape.Store == nil {
		return
	}
	files := a.recentReadFiles(tapeName)
	tools := a.DiscoveredToolNames(tapeName)
	archivedTools := a.archivedWindowToolNames(tapeName, meta)

	var b strings.Builder
	b.WriteString("[Post-Compact Context Rebuild]\n")
	writeArchivedWindowSection(&b, meta, archivedTools)
	if len(files) > 0 {
		b.WriteString("Recently accessed files:\n")
		for _, f := range files {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
	}
	if len(tools) > 0 {
		b.WriteString("Discovered tools (still available): ")
		b.WriteString(strings.Join(tools, ", "))
		b.WriteString("\n")
	}

	text := strings.TrimSpace(b.String())
	if text == "" || text == "[Post-Compact Context Rebuild]" {
		return
	}

	if err := a.tape.AppendEntry(tapeName, tape.NewSystemEntry(text)); err != nil {
		slog.Warn("post-compact: failed to append rebuild entry", "tape", tapeName, "error", err)
		return
	}
	slog.Info("post-compact: rebuilt context",
		"tape", tapeName, "files", len(files), "discovered_tools", len(tools))
}

func writeArchivedWindowSection(b *strings.Builder, meta compactAnchorMeta, archivedTools []string) {
	if meta.PreviousAnchorName == "" && meta.PreviousAnchorEntryID <= 0 && meta.AnchorUUID == "" {
		return
	}
	b.WriteString("Archived window: entries after anchor ")
	if meta.PreviousAnchorName != "" {
		b.WriteString(fmt.Sprintf("%q", meta.PreviousAnchorName))
	} else {
		b.WriteString("(previous anchor)")
	}
	if meta.PreviousAnchorUUID != "" {
		b.WriteString(fmt.Sprintf(" (uuid=%s", meta.PreviousAnchorUUID))
		if meta.PreviousAnchorEntryID > 0 {
			b.WriteString(fmt.Sprintf(", id=%d", meta.PreviousAnchorEntryID))
		}
		b.WriteString(")")
	} else if meta.PreviousAnchorEntryID > 0 {
		b.WriteString(fmt.Sprintf(" (id=%d)", meta.PreviousAnchorEntryID))
	}
	b.WriteString(" were summarized into this compact.\n")

	if meta.PreviousAnchorUUID != "" && meta.AnchorUUID != "" {
		b.WriteString("To retrieve full tool outputs from that window:\n")
		b.WriteString(fmt.Sprintf("  tapeSearch(between_uuid=\"%s,%s\", kinds=[\"tool_result\"], scope=\"all\")\n",
			meta.PreviousAnchorUUID, meta.AnchorUUID))
	} else {
		b.WriteString("Previous anchor has no UUID; use keyword search instead:\n")
		b.WriteString("  tapeSearch(scope=\"all\", kinds=[\"tool_result\"], query=\"<keywords>\")\n")
		b.WriteString("  tapeAnchors lists anchor names and UUIDs for other windows.\n")
	}
	if len(archivedTools) > 0 {
		b.WriteString("Tools used in the archived window (query hints): ")
		b.WriteString(strings.Join(archivedTools, ", "))
		b.WriteString("\n")
	}
	if meta.TriggerReason != "" {
		b.WriteString("Compact trigger: ")
		b.WriteString(meta.TriggerReason)
		b.WriteString("\n")
	}
}

// archivedWindowToolNames lists distinct tool names from tool_call entries in
// the compacted-away window (after previous anchor, before new compact anchor).
func (a *Agent) archivedWindowToolNames(tapeName string, meta compactAnchorMeta) []string {
	if meta.AnchorEntryID <= 0 && meta.PreviousAnchorEntryID <= 0 && meta.PreviousAnchorName == "" {
		return nil
	}
	entries, err := a.tape.Store.FetchAll(tapeName, &tape.FetchOpts{Kinds: []string{"tool_call"}})
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	for _, e := range entries {
		if e.ID <= meta.PreviousAnchorEntryID {
			continue
		}
		if meta.AnchorEntryID > 0 && e.ID >= meta.AnchorEntryID {
			continue
		}
		calls, ok := tape.ExtractToolCalls(e.Payload)
		if !ok {
			continue
		}
		for _, c := range calls {
			name := strings.TrimSpace(c.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
			if len(names) >= maxArchivedToolHint {
				return names
			}
		}
	}
	return names
}

// recentReadFiles scans recent tool_call entries and returns the most recently
// accessed file paths (deduped, newest first, capped at maxRecentFiles).
func (a *Agent) recentReadFiles(tapeName string) []string {
	entries, err := a.tape.Store.FetchAll(tapeName, &tape.FetchOpts{Kinds: []string{"tool_call"}})
	if err != nil {
		return nil
	}
	var paths []string
	seen := make(map[string]bool)
	// Iterate newest-first so the most recent reads survive dedup.
	for i := len(entries) - 1; i >= 0 && len(paths) < maxRecentFiles; i-- {
		calls, ok := tape.ExtractToolCalls(entries[i].Payload)
		if !ok {
			continue
		}
		for _, c := range calls {
			if !readToolNames[strings.ToLower(c.Name)] {
				continue
			}
			if p := extractFilePath(c.Arguments); p != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// extractFilePath parses a tool-call arguments JSON string and returns the
// first present file-path-like field.
func extractFilePath(argsJSON string) string {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filename", "file", "filepath"} {
		if v, ok := m[key].(string); ok {
			if p := strings.TrimSpace(v); p != "" {
				return p
			}
		}
	}
	return ""
}
