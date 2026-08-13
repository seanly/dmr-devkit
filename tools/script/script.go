// Package script bridges self-describing executables on the filesystem into
// *tool.Tool values, modelled after shell-operator's `--config` protocol:
//
//   - Config mode: `<script> --config` prints a JSON tool spec on stdout.
//   - Run mode:    `<script>` does the work. Input args and output result are
//     exchanged via env-var-pointed temp files (TOOL_ARGS_PATH /
//     TOOL_RESULT_PATH); stdout/stderr stay free for logging. Exit 0 = success,
//     non-zero = error.
//
// Each script is bridged into a *tool.Tool with Group=extended by default, so
// it is discovered on demand via ToolSearch — exactly like deferred built-in
// tools and external MCP tools.
package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/tool"
)

// defaultTimeout caps a single script-tool run when the script does not declare
// its own timeout. shell-operator has no execution timeout; we add one so a
// hung script cannot stall the agent loop indefinitely.
const defaultTimeout = 60 * time.Second

// configFetchTimeout caps the `<script> --config` probe.
const configFetchTimeout = 5 * time.Second

// dataExts are treated as data/library files, not tools.
var dataExts = map[string]bool{
	".md": true, ".yaml": true, ".yml": true, ".json": true, ".txt": true,
}

// defaultNamePrefix is prepended to every script tool's declared name. It
// namespaces script tools away from built-in tools (and MCP tools, which use
// the mcp_ prefix), so a script declaring name:"foo" becomes "script_foo" and
// can never collide. Scripts declare their logical name; the prefix is applied
// at load time.
const defaultNamePrefix = "script_"

// Spec is the JSON a script prints when invoked with `--config`.
type Spec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Group       string         `json:"group,omitempty"`   // default "extended"
	Timeout     string         `json:"timeout,omitempty"` // Go duration; default 60s
	SearchHint  string         `json:"search_hint,omitempty"`
}

// timeout parses the script-declared timeout, falling back to the default.
func (c Spec) timeout() time.Duration {
	if c.Timeout == "" {
		return defaultTimeout
	}
	if d, err := time.ParseDuration(c.Timeout); err == nil && d > 0 {
		return d
	}
	slog.Warn("script tool: invalid timeout, using default", "name", c.Name, "timeout", c.Timeout)
	return defaultTimeout
}

// group resolves the tool group, defaulting to extended (deferred discovery).
func (c Spec) group() tool.ToolGroup {
	switch tool.ToolGroup(c.Group) {
	case tool.ToolGroupCore, tool.ToolGroupExtended, tool.ToolGroupMCP:
		return tool.ToolGroup(c.Group)
	default:
		return tool.ToolGroupExtended
	}
}

// Options configures Load.
type Options struct {
	// NamePrefix is prepended to each tool's declared name. Defaults to
	// "script_" when empty.
	NamePrefix string

	// StaticEnv is injected into every script run, in addition to the standard
	// TOOL_* env vars. Use it for caller-specific context that does not change
	// per run, e.g. OKF_BUNDLE_ROOT.
	StaticEnv map[string]string
}

// Load discovers executable scripts under dir, probes each with `--config`, and
// returns one *tool.Tool per valid script. Scripts that fail discovery or
// `--config`, or whose names collide with an earlier script, are skipped with a
// warning — a single bad script never breaks agent startup.
func Load(ctx context.Context, dir string, opts Options) ([]*tool.Tool, error) {
	prefix := opts.NamePrefix
	if prefix == "" {
		prefix = defaultNamePrefix
	}

	paths, err := discoverScripts(dir)
	if err != nil {
		return nil, fmt.Errorf("discover script tools in %s: %w", dir, err)
	}

	var tools []*tool.Tool
	seen := make(map[string]bool)
	for _, p := range paths {
		spec, err := fetchScriptConfig(ctx, p)
		if err != nil {
			slog.Warn("script tool: --config failed, skipping", "path", p, "error", err)
			continue
		}
		if spec.Name == "" || spec.Parameters == nil {
			slog.Warn("script tool: spec missing name or parameters, skipping", "path", p)
			continue
		}
		if seen[spec.Name] {
			slog.Warn("script tool: duplicate name, skipping", "name", spec.Name, "path", p)
			continue
		}
		seen[spec.Name] = true
		tools = append(tools, newScriptTool(p, spec, prefix, opts.StaticEnv))
		slog.Info("script tool: loaded", "name", prefix+spec.Name, "path", p)
	}
	return tools, nil
}

// newScriptTool builds a *tool.Tool whose Handler runs the script in run mode.
// The script path is resolved to an absolute path up front so that the run-mode
// exec (which sets cmd.Dir to the script's directory) works regardless of how
// the tools dir was specified (relative or absolute).
func newScriptTool(path string, spec Spec, prefix string, staticEnv map[string]string) *tool.Tool {
	absPath := path
	if a, err := filepath.Abs(path); err == nil {
		absPath = a
	}
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        prefix + spec.Name,
			Description: spec.Description,
			Parameters:  spec.Parameters,
			Group:       spec.group(),
			SearchHint:  spec.SearchHint,
		},
		NeedContext: true,
		Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
			return runScriptTool(ctx, absPath, spec, staticEnv, args)
		},
	}
}

// runScriptTool executes the script in run mode using the env-var temp-file
// I/O contract.
func runScriptTool(ctx *tool.ToolContext, path string, spec Spec, staticEnv map[string]string, args map[string]any) (any, error) {
	parent := ctx.Ctx
	if parent == nil {
		parent = context.Background()
	}
	tCtx, cancel := context.WithTimeout(parent, spec.timeout())
	defer cancel()

	// Per-run temp dir holds the args and result files.
	tmpDir, err := os.MkdirTemp("", "script-tool-")
	if err != nil {
		return nil, fmt.Errorf("script %s: create tmp dir: %w", spec.Name, err)
	}
	defer os.RemoveAll(tmpDir)

	argsPath := filepath.Join(tmpDir, "args.json")
	resultPath := filepath.Join(tmpDir, "result.json")

	argsBytes, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("script %s: marshal args: %w", spec.Name, err)
	}
	if err := os.WriteFile(argsPath, argsBytes, 0o600); err != nil {
		return nil, fmt.Errorf("script %s: write args: %w", spec.Name, err)
	}
	// Pre-create the (empty) result file so scripts can rely on it existing.
	if err := os.WriteFile(resultPath, nil, 0o600); err != nil {
		return nil, fmt.Errorf("script %s: init result: %w", spec.Name, err)
	}

	cmd := exec.CommandContext(tCtx, path)
	cmd.Dir = filepath.Dir(path)
	cmd.Env = append(os.Environ(),
		"TOOL_ARGS_PATH="+argsPath,
		"TOOL_RESULT_PATH="+resultPath,
	)
	// Standard, provider-agnostic context env vars (match the TOOL_ prefix).
	if ctx.Workspace != "" {
		cmd.Env = append(cmd.Env, "TOOL_WORKSPACE="+ctx.Workspace)
	}
	if ctx.Tape != "" {
		cmd.Env = append(cmd.Env, "TOOL_TAPE="+ctx.Tape)
	}
	if ctx.RunID != "" {
		cmd.Env = append(cmd.Env, "TOOL_RUN_ID="+ctx.RunID)
	}
	// Caller-specific static env (e.g. OKF_BUNDLE_ROOT).
	for k, v := range staticEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	// Put the script in its own process group so a timeout kills it and any
	// children it spawned (e.g. `sleep`, `curl`), not just the shell.
	configureScriptExec(cmd)

	runErr := cmd.Run()

	// A context deadline surfaces as an *exec.ExitError on some platforms but
	// is more reliably detected via tCtx.Err().
	if tCtx.Err() == context.DeadlineExceeded {
		slog.Warn("script tool: timed out", "name", spec.Name, "path", path,
			"timeout", spec.timeout(), "output", strings.TrimSpace(out.String()))
		return nil, fmt.Errorf("script %s timed out after %s", spec.Name, spec.timeout())
	}
	if runErr != nil {
		slog.Warn("script tool: run failed", "name", spec.Name, "path", path,
			"error", runErr, "output", strings.TrimSpace(out.String()))
		return nil, fmt.Errorf("script %s exited non-zero: %s", spec.Name, tailOutput(out.String(), 512))
	}

	result, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, fmt.Errorf("script %s: read result: %w", spec.Name, err)
	}
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 {
		// Script succeeded but wrote nothing — treat as a null result.
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, fmt.Errorf("script %s: invalid result JSON: %w", spec.Name, err)
	}
	slog.Info("script tool: ran", "name", spec.Name, "path", path,
		"output_bytes", len(trimmed), "log", strings.TrimSpace(out.String()))
	return decoded, nil
}

// fetchScriptConfig runs `<path> --config` and parses the JSON spec from stdout.
func fetchScriptConfig(ctx context.Context, path string) (Spec, error) {
	cctx, cancel := context.WithTimeout(ctx, configFetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, path, "--config")
	out, err := cmd.Output()
	if err != nil {
		return Spec{}, fmt.Errorf("run --config: %w", err)
	}
	var spec Spec
	if err := json.Unmarshal(bytes.TrimSpace(out), &spec); err != nil {
		return Spec{}, fmt.Errorf("parse --config output: %w", err)
	}
	return spec, nil
}

// discoverScripts recursively walks dir for executable script files, applying
// shell-operator's discovery rules: skip hidden entries, a top-level lib/
// directory, data extensions, and non-executable files. Results are sorted by
// path so numeric prefixes (001-, 002-) yield deterministic load order.
func discoverScripts(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		// Skip hidden files/dirs anywhere in the tree.
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip a top-level lib/ directory (shared libraries, not tools).
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() && rel == "lib" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if dataExts[strings.ToLower(filepath.Ext(name))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // unreachable entry: skip, don't abort the walk
		}
		if !isExecutable(info) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// isExecutable reports whether info describes a user/group/other-executable
// regular file.
func isExecutable(info os.FileInfo) bool {
	if info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// tailOutput returns the last ~n bytes of s, trimmed, for compact error
// messages.
func tailOutput(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
