// Package fs provides file system tools (fsRead, fsWrite, fsEdit, fsGrep, fsGlob, fsList)
// for AI agent file operations. Call Tools() to obtain the tool set for
// tool.Executor / devkit.Options.Tools.
package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tool"
)

// Tools returns the six core filesystem tools.
func Tools() []*tool.Tool {
	return []*tool.Tool{
		readTool(),
		writeTool(),
		editTool(),
		grepTool(),
		globTool(),
		listTool(),
	}
}

func readTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "fsRead",
			Description: "Read a text file. Supports optional pagination with offset and limit.",
			Group:       tool.ToolGroupCore, // Core tool - always available
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "File path"},
					"offset": map[string]any{"type": "integer", "default": 0, "description": "Line offset"},
					"limit":  map[string]any{"type": "integer", "description": "Max lines to read"},
				},
				"required": []string{"path"},
			},
		},
		Handler:     readHandler,
		NeedContext: true,
	}
}

func readHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	pathStr, ok := args["path"].(string)
	if !ok || pathStr == "" {
		return nil, core.MakeErrToolExec("fsRead", fmt.Errorf("path is required and must be a string")).Build()
	}
	path, err := resolvePath(ctx, pathStr)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsRead").Build()
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsRead").Build()
	}
	lines := strings.Split(string(text), "\n")
	offset := 0
	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := len(lines)
	if l, ok := args["limit"].(float64); ok && int(l) > 0 {
		if offset+int(l) < end {
			end = offset + int(l)
		}
	}
	return strings.Join(lines[offset:end], "\n"), nil
}

func writeTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "fsWrite",
			Description: "Write content to a text file.",
			Group:       tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "File path"},
					"content": map[string]any{"type": "string", "description": "Content to write"},
				},
				"required": []string{"path", "content"},
			},
		},
		Handler:     writeHandler,
		NeedContext: true,
	}
}

func writeHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	pathStr, ok := args["path"].(string)
	if !ok || pathStr == "" {
		return nil, core.MakeErrToolExec("fsWrite", fmt.Errorf("path is required and must be a string")).Build()
	}
	path, err := resolvePath(ctx, pathStr)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsWrite").Build()
	}
	content, ok := args["content"].(string)
	if !ok {
		return nil, core.MakeErrToolExec("fsWrite", fmt.Errorf("content is required and must be a string")).Build()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, core.FromError(err).With("tool", "fsWrite").Build()
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, core.FromError(err).With("tool", "fsWrite").Build()
	}
	return fmt.Sprintf("wrote: %s", path), nil
}

func editTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "fsEdit",
			Description: "Edit a text file by replacing old text with new text.",
			Group:       tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]any{"type": "string", "description": "File path"},
					"old":   map[string]any{"type": "string", "description": "Text to find"},
					"new":   map[string]any{"type": "string", "description": "Replacement text"},
					"start": map[string]any{"type": "integer", "default": 0, "description": "Line to start searching from"},
				},
				"required": []string{"path", "old", "new"},
			},
		},
		Handler:     editHandler,
		NeedContext: true,
	}
}

func editHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	pathStr, ok := args["path"].(string)
	if !ok || pathStr == "" {
		return nil, core.MakeErrToolExec("fsEdit", fmt.Errorf("path is required and must be a string")).Build()
	}
	path, err := resolvePath(ctx, pathStr)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsEdit").Build()
	}
	old, ok := args["old"].(string)
	if !ok {
		return nil, core.MakeErrToolExec("fsEdit", fmt.Errorf("old is required and must be a string")).Build()
	}
	newText, ok := args["new"].(string)
	if !ok {
		return nil, core.MakeErrToolExec("fsEdit", fmt.Errorf("new is required and must be a string")).Build()
	}
	start := 0
	if s, ok := args["start"].(float64); ok {
		start = int(s)
	}

	text, err := os.ReadFile(path)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsEdit").Build()
	}
	lines := strings.Split(string(text), "\n")
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	prev := strings.Join(lines[:start], "\n")
	toReplace := strings.Join(lines[start:], "\n")

	if !strings.Contains(toReplace, old) {
		return nil, core.MakeErrToolExec("fsEdit", fmt.Errorf("'%s' not found in %s from line %d", old, path, start)).Build()
	}

	replaced := strings.Replace(toReplace, old, newText, 1)
	if prev != "" {
		replaced = prev + "\n" + replaced
	}
	if err := os.WriteFile(path, []byte(replaced), 0o600); err != nil {
		return nil, core.FromError(err).With("tool", "fsEdit").Build()
	}
	return fmt.Sprintf("edited: %s", path), nil
}

func resolvePath(ctx *tool.ToolContext, raw string) (string, error) {
	ws := ctx.GetCwd()
	if ws == "" {
		return "", fmt.Errorf("fs plugin requires a workspace to be configured")
	}

	wsClean := filepath.Clean(ws)
	wsReal, err := filepath.EvalSymlinks(wsClean)
	if err != nil {
		wsReal = wsClean
	}

	var resolved string
	if filepath.IsAbs(raw) {
		resolved = filepath.Clean(raw)
	} else {
		resolved = filepath.Clean(filepath.Join(wsClean, raw))
		// Relative paths are resolved against the workspace; reject traversal
		// that escapes the workspace via "..". Absolute paths outside the
		// workspace are allowed so that policy (not the fs plugin) decides
		// whether reads outside the workspace require approval.
		sep := string(filepath.Separator)
		if resolved != wsClean && !strings.HasPrefix(resolved, wsClean+sep) {
			return "", fmt.Errorf("path %q escapes workspace %q", resolved, wsClean)
		}
	}

	// Resolve symlinks in the target path to detect escapes from the workspace.
	// Explicit absolute paths outside the workspace are allowed (OPA policy
	// decides whether they require approval); relative paths must not escape.
	targetReal, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		// File doesn't exist (e.g. for write): walk up to find the nearest
		// existing ancestor and resolve symlinks from there.
		targetReal = evalSymlinksWalkUp(resolved)
	}

	sep := string(filepath.Separator)
	if targetReal != wsReal && !strings.HasPrefix(targetReal, wsReal+sep) {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf("path %q escapes workspace %q", targetReal, wsReal)
		}
	}

	// Return the original resolved path (not the physical path) for caller consistency.
	return resolved, nil
}

// evalSymlinksWalkUp resolves symlinks by walking up the directory tree until
// it finds an existing ancestor. This handles the case where intermediate
// directories don't exist yet (e.g. fsWrite creating subdir/file.txt).
func evalSymlinksWalkUp(path string) string {
	var tail []string
	cur := path
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			// Found an existing ancestor; rebuild the full path.
			result := real
			for i := len(tail) - 1; i >= 0; i-- {
				result = filepath.Join(result, tail[i])
			}
			return result
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root without finding anything; return original.
			return path
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}
