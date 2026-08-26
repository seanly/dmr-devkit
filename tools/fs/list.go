package fs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tool"
)

const defaultListHeadLimit = 500

func listTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "fsList",
			Description: "List directory entries in the workspace. Non-recursive by default; set recursive=true for a depth-first walk.",
			Group:       tool.ToolGroupCore,
			SearchHint:  "list, ls, directory, dir, explore, files, folders",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]any{"type": "string", "default": ".", "description": "Directory path relative to workspace"},
					"recursive":  map[string]any{"type": "boolean", "default": false, "description": "Recursively list all entries"},
					"head_limit": map[string]any{"type": "integer", "default": defaultListHeadLimit, "description": "Max entries to return (recursive mode)"},
				},
			},
		},
		Handler:     listHandler,
		NeedContext: true,
	}
}

func listHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	rawPath := stringArg(args, "path")
	if rawPath == "" {
		rawPath = "."
	}
	dir, err := resolvePath(ctx, rawPath)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsList").Build()
	}

	info, err := os.Stat(dir)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsList").Build()
	}
	if !info.IsDir() {
		return nil, core.MakeErrToolExec("fsList", fmt.Errorf("%q is not a directory", rawPath)).Build()
	}

	if !boolArg(args, "recursive") {
		return listOneLevel(dir)
	}

	headLimit := intArg(args, "head_limit", defaultListHeadLimit)
	if headLimit <= 0 {
		headLimit = defaultListHeadLimit
	}
	return listRecursive(dir, headLimit)
}

func listOneLevel(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			lines = append(lines, "dir/ "+name)
		} else {
			lines = append(lines, "file "+name)
		}
	}
	return strings.Join(lines, "\n"), nil
}

func listRecursive(root string, headLimit int) (string, error) {
	var lines []string
	truncated := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		if d.IsDir() {
			lines = append(lines, "dir/ "+rel)
		} else {
			lines = append(lines, "file "+rel)
		}
		if len(lines) >= headLimit {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return "", err
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += fmt.Sprintf("\n... truncated at %d entries", headLimit)
	}
	if len(out) > maxOutputChars {
		out = out[:maxOutputChars] + "\n... truncated"
	}
	return out, nil
}
