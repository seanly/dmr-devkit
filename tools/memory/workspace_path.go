package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/seanly/dmr-devkit/tool"
)

func resolveWorkspacePath(ctx *tool.ToolContext, raw string) (string, error) {
	ws := ctx.GetCwd()
	if ws == "" {
		return "", fmt.Errorf("memory attachment tools require a workspace to be configured")
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
	}
	targetReal, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		targetReal = evalSymlinksWalkUpMemory(resolved)
	}
	sep := string(filepath.Separator)
	if targetReal != wsReal && !strings.HasPrefix(targetReal, wsReal+sep) {
		return "", fmt.Errorf("path %q escapes workspace %q", targetReal, wsReal)
	}
	return resolved, nil
}

func evalSymlinksWalkUpMemory(path string) string {
	var tail []string
	cur := path
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			result := real
			for i := len(tail) - 1; i >= 0; i-- {
				result = filepath.Join(result, tail[i])
			}
			return result
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func writeWorkspaceFile(ctx *tool.ToolContext, relPath string, data []byte) (abs string, err error) {
	absPath, err := resolveWorkspacePath(ctx, relPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(absPath, data, 0o600); err != nil {
		return "", err
	}
	return absPath, nil
}
