package fs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/seanly/dmr-devkit/tool"
)

const (
	defaultHeadLimit = 250
	maxOutputChars   = 20_000
)

var (
	rgPath     string
	rgPathOnce sync.Once
	rgPathErr  error
)

func findRg() (string, error) {
	rgPathOnce.Do(func() {
		rgPath, rgPathErr = exec.LookPath("rg")
	})
	return rgPath, rgPathErr
}

func rgAvailable() bool {
	_, err := findRg()
	return err == nil
}

type grepOutputMode string

const (
	grepModeFiles   grepOutputMode = "files_with_matches"
	grepModeContent grepOutputMode = "content"
	grepModeCount   grepOutputMode = "count"
)

type grepOptions struct {
	pattern       string
	searchPath    string
	glob          string
	outputMode    grepOutputMode
	headLimit     int
	offset        int
	contextLines  int
	beforeLines   int
	afterLines    int
	caseSensitive bool
	multiline     bool
}

type globOptions struct {
	pattern    string
	searchPath string
	headLimit  int
	offset     int
}

func resolveSearchPath(ctx *tool.ToolContext, raw string, defaultDot bool) (string, error) {
	if raw == "" {
		if defaultDot {
			return resolvePath(ctx, ".")
		}
		ws := ctx.GetCwd()
		if ws == "" {
			return "", fmt.Errorf("fs plugin requires a workspace to be configured")
		}
		return ws, nil
	}
	return resolvePath(ctx, raw)
}

func intArg(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

func boolArg(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func buildGrepArgs(opts grepOptions) []string {
	args := []string{"--color=never", "--no-heading"}
	switch opts.outputMode {
	case grepModeFiles:
		args = append(args, "--files-with-matches")
	case grepModeCount:
		args = append(args, "--count-matches")
	default:
		args = append(args, "--line-number")
	}
	if !opts.caseSensitive {
		args = append(args, "-i")
	}
	if opts.multiline {
		args = append(args, "-U", "--multiline-dotall")
	}
	if opts.contextLines > 0 {
		args = append(args, "-C", fmt.Sprintf("%d", opts.contextLines))
	} else {
		if opts.beforeLines > 0 {
			args = append(args, "-B", fmt.Sprintf("%d", opts.beforeLines))
		}
		if opts.afterLines > 0 {
			args = append(args, "-A", fmt.Sprintf("%d", opts.afterLines))
		}
	}
	if opts.glob != "" {
		args = append(args, "--glob", opts.glob)
	}
	args = append(args, "--", opts.pattern, opts.searchPath)
	return args
}

func buildGlobArgs(opts globOptions) []string {
	args := []string{"--color=never", "--files", "--glob", opts.pattern, opts.searchPath}
	return args
}

func runRg(ctx context.Context, rgArgs []string) (string, error) {
	rg, err := findRg()
	if err != nil {
		return "", fmt.Errorf("ripgrep (rg) not found in PATH; install ripgrep to use fsGrep/fsGlob")
	}
	cmd := exec.CommandContext(ctx, rg, rgArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// rg exit 1 = no matches
			return "", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("rg: %s", msg)
	}
	return stdout.String(), nil
}

func paginateOutput(raw string, offset, headLimit int) (string, bool) {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return "", false
	}
	lines := strings.Split(raw, "\n")
	total := len(lines)
	if offset > 0 {
		if offset >= total {
			return "", total > 0
		}
		lines = lines[offset:]
	}
	truncated := false
	if headLimit > 0 && len(lines) > headLimit {
		lines = lines[:headLimit]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxOutputChars {
		out = out[:maxOutputChars]
		truncated = true
	}
	if truncated {
		remaining := total - offset - len(lines)
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 0 {
			out += fmt.Sprintf("\n... truncated, %d more result(s)", remaining)
		} else {
			out += "\n... truncated"
		}
	}
	return out, truncated
}

func runGrep(ctx context.Context, opts grepOptions) (string, error) {
	out, err := runRg(ctx, buildGrepArgs(opts))
	if err != nil {
		return "", err
	}
	result, _ := paginateOutput(out, opts.offset, opts.headLimit)
	return result, nil
}

func runGlob(ctx context.Context, opts globOptions) (string, error) {
	out, err := runRg(ctx, buildGlobArgs(opts))
	if err != nil {
		return "", err
	}
	result, _ := paginateOutput(out, opts.offset, opts.headLimit)
	return result, nil
}
