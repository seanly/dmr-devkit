package agent

import (
	"context"
	"encoding/json"
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

// maxRecentFiles caps how many recently-accessed file paths are reattached.
const maxRecentFiles = 5

// rebuildPostCompactContext re-injects a minimal working environment after a
// compact so the model does not start from an empty context. It appends a
// single system entry to the tape summarizing:
//   - recently accessed file paths (extracted from tool_call entries)
//   - the names of tools already discovered in this session
//
// This mirrors Claude Code's post-compact reconstruction (recent files, tools,
// MCP). It is best-effort: when nothing is available, no entry is written.
func (a *Agent) rebuildPostCompactContext(ctx context.Context, tapeName string) {
	if a.tape == nil || a.tape.Store == nil {
		return
	}
	files := a.recentReadFiles(tapeName)
	tools := a.DiscoveredToolNames(tapeName)
	if len(files) == 0 && len(tools) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("[Post-Compact Context Rebuild]\n")
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
	if text == "" {
		return
	}

	if err := a.tape.AppendEntry(tapeName, tape.NewSystemEntry(text)); err != nil {
		slog.Warn("post-compact: failed to append rebuild entry", "tape", tapeName, "error", err)
		return
	}
	slog.Info("post-compact: rebuilt context",
		"tape", tapeName, "files", len(files), "discovered_tools", len(tools))
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
