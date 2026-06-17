package handoff

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/tape"
)

// UpdaterOptions configures heuristic state updates.
type UpdaterOptions struct {
	MaxArtifacts   int
	MaxActiveFiles int
}

// DefaultUpdaterOptions returns sensible defaults.
func DefaultUpdaterOptions() UpdaterOptions {
	return UpdaterOptions{MaxArtifacts: 20, MaxActiveFiles: 10}
}

// Updater maintains task state from tape entries.
type Updater struct {
	opts UpdaterOptions
}

// NewUpdater creates a heuristic state updater.
func NewUpdater(opts UpdaterOptions) *Updater {
	if opts.MaxArtifacts <= 0 {
		opts.MaxArtifacts = 20
	}
	if opts.MaxActiveFiles <= 0 {
		opts.MaxActiveFiles = 10
	}
	return &Updater{opts: opts}
}

// UpdateFromToolRound updates state after a tool execution round.
func (u *Updater) UpdateFromToolRound(prev *State, entries []tape.TapeEntry, step int, source string) State {
	base := State{}
	if prev != nil {
		base = *prev
	} else {
		base = u.inferInitialGoal(entries)
	}
	base.SchemaVersion = SchemaVersion
	base.Source = source
	base.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Scan recent entries for tool calls and user constraint changes.
	for i := len(entries) - 1; i >= 0 && i >= len(entries)-15; i-- {
		e := entries[i]
		switch e.Kind {
		case "message":
			if role, _ := e.Payload["role"].(string); role == "user" {
				if content, ok := e.Payload["content"].(string); ok {
					u.applyUserMessage(&base, content)
				}
			}
		case "tool_call":
			calls, ok := tape.ExtractToolCalls(e.Payload)
			if !ok {
				continue
			}
			for _, c := range calls {
				action := formatToolAction(c.Name, c.Arguments)
				base.LastAction = action
				u.collectArtifacts(&base, c.Name, c.Arguments)
			}
		}
	}
	base.ActiveFiles = u.recentFilePaths(base.Artifacts)
	return base
}

// SnapshotForHandoff clones state for handoff with updated source.
func (u *Updater) SnapshotForHandoff(prev *State, source string) State {
	if prev == nil {
		return NewState("(continuing task)", source)
	}
	out := *prev
	out.Source = source
	out.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return out
}

// ResetForTopicSwitch clears task fields for a new topic boundary.
func (u *Updater) ResetForTopicSwitch(newGoal, source string) State {
	return NewState(newGoal, source)
}

func (u *Updater) inferInitialGoal(entries []tape.TapeEntry) State {
	for _, e := range entries {
		if e.Kind != "message" {
			continue
		}
		if role, _ := e.Payload["role"].(string); role != "user" {
			continue
		}
		if content, ok := e.Payload["content"].(string); ok && strings.TrimSpace(content) != "" {
			goal := content
			if len([]rune(goal)) > 200 {
				goal = string([]rune(goal)[:200])
			}
			return NewState(goal, "heuristic")
		}
	}
	return NewState("(task in progress)", "heuristic")
}

var constraintPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)改成\s*(.+)`),
	regexp.MustCompile(`(?i)改为\s*(.+)`),
	regexp.MustCompile(`(?i)不要\s*(.+)`),
	regexp.MustCompile(`(?i)instead\s+(.+)`),
	regexp.MustCompile(`(?i)change\s+(?:to|it to)\s+(.+)`),
}

func (u *Updater) applyUserMessage(s *State, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	if s.Constraints == nil {
		s.Constraints = map[string]string{}
	}
	for _, re := range constraintPatterns {
		if m := re.FindStringSubmatch(content); len(m) > 1 {
			s.Constraints["latest"] = strings.TrimSpace(m[1])
			s.Constraints["scope"] = "session"
			return
		}
	}
}

func formatToolAction(name, argsJSON string) string {
	name = strings.TrimSpace(name)
	if argsJSON == "" || argsJSON == "{}" {
		return name + "()"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return name + "(...)"
	}
	for _, key := range []string{"path", "file", "query", "pattern", "command", "name"} {
		if v, ok := args[key]; ok {
			return name + "(" + stringifyArg(v) + ")"
		}
	}
	return name + "(...)"
}

func stringifyArg(v any) string {
	s := strings.TrimSpace(fmtAny(v))
	if len([]rune(s)) > 80 {
		return string([]rune(s)[:80]) + "..."
	}
	return s
}

func fmtAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

var pathLike = regexp.MustCompile(`([\./~][\w./_-]+\.(?:go|md|toml|yaml|yml|json|ts|tsx|js|py))`)

func (u *Updater) collectArtifacts(s *State, toolName, argsJSON string) {
	toolName = strings.ToLower(toolName)
	var paths []string
	if argsJSON != "" {
		var args map[string]any
		_ = json.Unmarshal([]byte(argsJSON), &args)
		for _, key := range []string{"path", "file", "pattern", "command"} {
			if v, ok := args[key]; ok {
				paths = append(paths, extractPaths(stringifyArg(v))...)
			}
		}
	}
	if len(paths) == 0 && (strings.Contains(toolName, "fs") || strings.Contains(toolName, "shell")) {
		paths = extractPaths(argsJSON)
	}
	for _, p := range paths {
		u.addArtifact(s, "file", p, "")
	}
}

func extractPaths(s string) []string {
	matches := pathLike.FindAllStringSubmatch(s, -1)
	var out []string
	seen := map[string]struct{}{}
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		p := strings.TrimSpace(m[1])
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func (u *Updater) addArtifact(s *State, typ, ref, label string) {
	for _, a := range s.Artifacts {
		if a.Type == typ && a.Ref == ref {
			return
		}
	}
	s.Artifacts = append(s.Artifacts, Artifact{Type: typ, Ref: ref, Label: label})
	if len(s.Artifacts) > u.opts.MaxArtifacts {
		s.Artifacts = s.Artifacts[len(s.Artifacts)-u.opts.MaxArtifacts:]
	}
}

func (u *Updater) recentFilePaths(artifacts []Artifact) []string {
	var files []string
	for i := len(artifacts) - 1; i >= 0; i-- {
		if artifacts[i].Type != "file" {
			continue
		}
		files = append(files, artifacts[i].Ref)
		if len(files) >= u.opts.MaxActiveFiles {
			break
		}
	}
	// reverse to chronological
	for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
		files[i], files[j] = files[j], files[i]
	}
	return files
}
