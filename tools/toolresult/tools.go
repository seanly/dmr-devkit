// Package toolresult provides scoped read/grep tools for persisted tool outputs
// under {workspace}/.dmr/tool-results/. Call Tools() (or ToolsWith) and append
// to [github.com/seanly/dmr-devkit/devkit.Options.Tools]. They are not registered
// by [github.com/seanly/dmr-devkit/devkit.Build].
package toolresult

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	persist "github.com/seanly/dmr-devkit/agent/toolresult"
	"github.com/seanly/dmr-devkit/tool"
)

const (
	ToolRead = "tool_result_read"
	ToolGrep = "tool_result_grep"

	readDefaultLimit = 200
	readMaxLimit     = 2000
	grepDefaultHead  = 50
	grepMaxHead      = 500
	maxLineBytes     = 1024 * 1024
)

type options struct {
	persistSubdir string
}

// Tools returns tool_result_read and tool_result_grep scoped to
// [persist.DefaultPersistSubdir] (`.dmr/tool-results`).
func Tools() []*tool.Tool {
	return ToolsWith("")
}

// ToolsWith is like Tools but scopes paths to persistSubdir under the workspace.
// Empty persistSubdir uses [persist.DefaultPersistSubdir].
func ToolsWith(persistSubdir string) []*tool.Tool {
	o := options{persistSubdir: strings.TrimSpace(persistSubdir)}
	if o.persistSubdir == "" {
		o.persistSubdir = persist.DefaultPersistSubdir
	}
	return []*tool.Tool{
		o.readTool(),
		o.grepTool(),
	}
}

func (o options) readTool() *tool.Tool {
	sub := filepath.ToSlash(o.persistSubdir)
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name: ToolRead,
			Description: "Read a persisted tool output file under " + sub +
				"/. Use offset/limit to page; do not dump the whole file. Path is relative to the workspace (as shown in <persisted-output>).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path from <persisted-output>, e.g. " + sub + "/<tape>/<tool_call_id>.txt",
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "0-based line offset (default 0).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max lines to return (default 200, max 2000).",
					},
				},
				"required": []any{"path"},
			},
		},
		NeedContext: true,
		Handler:     o.readHandler,
	}
}

func (o options) grepTool() *tool.Tool {
	sub := filepath.ToSlash(o.persistSubdir)
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name: ToolGrep,
			Description: "Regex-search persisted tool outputs under " + sub +
				"/. Prefer this over re-running the original dump. Empty path searches the whole persist tree.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Regular expression to search for.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "File or directory under " + sub + "/. Omit to search all persisted outputs.",
					},
					"head_limit": map[string]any{
						"type":        "integer",
						"description": "Max matching lines to return (default 50, max 500).",
					},
				},
				"required": []any{"pattern"},
			},
		},
		NeedContext: true,
		Handler:     o.grepHandler,
	}
}

func (o options) readHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	pathStr, _ := args["path"].(string)
	abs, rel, err := o.resolvePath(ctx, pathStr, false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ToolRead, err)
	}
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	limit := intArg(args, "limit", readDefaultLimit)
	if limit <= 0 {
		limit = readDefaultLimit
	}
	if limit > readMaxLimit {
		limit = readMaxLimit
	}

	lines, total, err := readLines(abs, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ToolRead, err)
	}
	end := offset + len(lines)
	var b strings.Builder
	fmt.Fprintf(&b, "%s lines %d-%d of %d\n", rel, offset, end, total)
	if end < total {
		fmt.Fprintf(&b, "(more lines remain; pass offset=%d)\n", end)
	}
	b.WriteString("\n")
	b.WriteString(strings.Join(lines, "\n"))
	return b.String(), nil
}

func (o options) grepHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	pattern, _ := args["pattern"].(string)
	if strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("%s: pattern is required", ToolGrep)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid pattern: %w", ToolGrep, err)
	}
	head := intArg(args, "head_limit", grepDefaultHead)
	if head <= 0 {
		head = grepDefaultHead
	}
	if head > grepMaxHead {
		head = grepMaxHead
	}

	pathStr, _ := args["path"].(string)
	abs, displayRel, err := o.resolvePath(ctx, pathStr, true)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ToolGrep, err)
	}

	matches, truncated, err := o.grepResults(abs, displayRel, re, head)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ToolGrep, err)
	}
	if len(matches) == 0 {
		return "no matches", nil
	}
	out := strings.Join(matches, "\n")
	if truncated {
		out += fmt.Sprintf("\n[truncated at %d matches]", head)
	}
	return out, nil
}

// resolvePath resolves path under {workspace}/{persistSubdir}.
// Empty path is allowed only when allowEmptyDir is true (grep the whole tree).
func (o options) resolvePath(ctx *tool.ToolContext, rel string, allowEmptyDir bool) (absPath, displayRel string, err error) {
	workspace := ""
	if ctx != nil {
		workspace = strings.TrimSpace(ctx.Workspace)
		if workspace == "" {
			workspace = strings.TrimSpace(ctx.GetCwd())
		}
	}
	if workspace == "" {
		return "", "", fmt.Errorf("workspace is not configured")
	}
	wsAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace: %w", err)
	}
	rootAbs, err := filepath.Abs(filepath.Join(wsAbs, filepath.FromSlash(o.persistSubdir)))
	if err != nil {
		return "", "", err
	}

	rel = strings.TrimSpace(rel)
	if rel == "" {
		if !allowEmptyDir {
			return "", "", fmt.Errorf("path is required")
		}
		return rootAbs, filepath.ToSlash(o.persistSubdir), nil
	}

	candidate := rel
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(wsAbs, candidate)
	}
	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", err
	}
	if err := containPath(rootAbs, resolved, o.persistSubdir); err != nil {
		return "", "", err
	}

	info, err := os.Lstat(resolved)
	if err != nil {
		return "", "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("symlinks are not allowed")
	}
	if !allowEmptyDir && !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("not a regular file")
	}
	if allowEmptyDir && !info.Mode().IsRegular() && !info.IsDir() {
		return "", "", fmt.Errorf("not a file or directory")
	}

	display, err := filepath.Rel(wsAbs, resolved)
	if err != nil {
		display = resolved
	}
	return resolved, filepath.ToSlash(display), nil
}

func containPath(root, candidate, persistSubdir string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("path escapes %s", persistSubdir)
	}
	return nil
}

func readLines(path string, offset, limit int) (lines []string, total int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)

	n := 0
	for scanner.Scan() {
		if n >= offset && len(lines) < limit {
			lines = append(lines, scanner.Text())
		}
		n++
	}
	if err := scanner.Err(); err != nil {
		return nil, n, err
	}
	return lines, n, nil
}

func (o options) grepResults(root, displayRel string, re *regexp.Regexp, head int) (matches []string, truncated bool, err error) {
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("symlinks are not allowed")
	}
	if info.Mode().IsRegular() {
		return grepFile(root, displayRel, re, head)
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if truncated {
			return fs.SkipAll
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := containPath(root, path, o.persistSubdir); err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		display := filepath.ToSlash(filepath.Join(displayRel, rel))
		found, hitLimit, gerr := grepFile(path, display, re, head-len(matches))
		if gerr != nil {
			return gerr
		}
		matches = append(matches, found...)
		if hitLimit || len(matches) >= head {
			truncated = true
			if len(matches) > head {
				matches = matches[:head]
			}
			return fs.SkipAll
		}
		return nil
	})
	return matches, truncated, err
}

func grepFile(path, displayRel string, re *regexp.Regexp, remaining int) (matches []string, truncated bool, err error) {
	if remaining <= 0 {
		return nil, true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		text := scanner.Text()
		if !re.MatchString(text) {
			continue
		}
		matches = append(matches, fmt.Sprintf("%s:%d:%s", displayRel, lineNo, text))
		if len(matches) >= remaining {
			return matches, true, scanner.Err()
		}
	}
	return matches, false, scanner.Err()
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return def
		}
		return int(i)
	default:
		return def
	}
}
