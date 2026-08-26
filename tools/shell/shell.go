//go:build !windows

// Package shell provides Unix shell execution tools (shell, shellOutput, shellKill, shellJobs).
package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tool"
)

// Manager holds background jobs and tool handlers.
type Manager struct {
	shellManager   *ShellManager
	interactive    bool
	defaultTimeout int
	requireReason  bool
}

// New creates a Manager. Timeout defaults to 30 seconds when unset or non-positive.
func New(opts Options) *Manager {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	return &Manager{
		shellManager:   NewShellManager(),
		interactive:    opts.Interactive,
		defaultTimeout: timeout,
		requireReason:  opts.RequireReason,
	}
}

// Tools returns the four shell tools.
func (m *Manager) Tools() []*tool.Tool {
	return []*tool.Tool{
		m.shellTool(),
		m.shellOutputTool(),
		m.shellKillTool(),
		m.shellJobsTool(),
	}
}

// Close terminates all background jobs.
func (m *Manager) Close() error {
	return m.shellManager.ShutdownAll()
}

func (m *Manager) shellTool() *tool.Tool {
	required := []string{"cmd"}
	if m.requireReason {
		required = append(required, "reason")
	}

	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "shell",
			Description: "Execute a shell command. When invoking this tool, you MUST briefly explain your intent in the 'reason' parameter. Prefer commands grounded in official documentation for the system you are using, or steps from a skill you have already loaded via the skill tool.",
			Group:       tool.ToolGroupCore, // Core tool - always available
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cmd":             map[string]any{"type": "string", "description": "Command to execute. Prefer verified documentation for the target system, or a skill you have loaded; avoid guessing API paths or JSON fields."},
					"cwd":             map[string]any{"type": "string", "description": "Working directory"},
					"timeout_seconds": map[string]any{"type": "integer", "default": 30},
					"background":      map[string]any{"type": "boolean", "default": false},
					"reason":          map[string]any{"type": "string", "description": "Explain why this command is needed and what you intend to achieve"},
					"credential_bindings": map[string]any{
						"type":        "array",
						"description": "Bind stored credentials to environment variables. Use credentialsSearch (preferred when many exist) or credentialsList, then credentialsDescribe, to discover credentials and fields.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"credential_id": map[string]any{"type": "string", "description": "Credential ID from the store"},
								"env":           map[string]any{"type": "string", "description": "Target env var name (for secretText/secretFile)"},
								"env_map":       map[string]any{"type": "object", "description": "Field-to-env-var mapping (for usernamePassword/sshPrivateKey), e.g. {\"username\": \"PGUSER\", \"password\": \"PGPASSWORD\"}"},
							},
							"required": []string{"credential_id"},
						},
					},
				},
				"required": required,
			},
		},
		Handler:     m.execHandler,
		NeedContext: true,
	}
}

func (m *Manager) execHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	cmd, _ := args["cmd"].(string)
	cwd, _ := args["cwd"].(string)
	timeout := m.defaultTimeout
	if t, ok := args["timeout_seconds"].(float64); ok {
		timeout = int(t)
	}
	background, _ := args["background"].(bool)

	// Determine working directory with CWD management
	cwd = m.determineCwd(ctx, cwd)

	// Check if CWD is allowed (directory escape prevention)
	if err := ctx.CheckCwdAllowed(cwd); err != nil {
		return nil, core.MakeErrToolExec("shell", fmt.Errorf("cwd not allowed: %w", err)).Build()
	}

	// Recover if CWD was deleted (e.g., by a previous command)
	if ctx.CwdManager != nil {
		recoveredCwd, recovered, err := ctx.RecoverCwd()
		if err != nil {
			return nil, core.MakeErrToolExec("shell", fmt.Errorf("cwd recovery failed: %w", err)).Build()
		}
		if recovered {
			// If we recovered, use the recovered CWD
			cwd = recoveredCwd
		}
	}

	extraEnv, tempFiles := extractRuntimeInject(args)

	// Handle CWD policy
	if ctx.CwdPolicy == tool.CwdPolicyPrevent {
		// Prevent CWD changes by running in a sandboxed environment
		// This is a simplified implementation; full sandbox would use chroot/containers
		extraEnv["DMR_CWD_LOCKED"] = "1"
	}

	if background {
		shellID := m.shellManager.Start(cmd, cwd, m.interactive, extraEnv, tempFiles)
		return fmt.Sprintf("started: %s", shellID), nil
	}

	var baseCtx context.Context
	if ctx == nil || ctx.Ctx == nil {
		baseCtx = context.Background()
	} else {
		baseCtx = ctx.Ctx
	}

	output, exitCode, err := executeCommand(baseCtx, cmd, cwd, timeout, m.interactive, extraEnv, tempFiles)
	if err != nil {
		return nil, core.FromError(err).With("tool", "shell").Build()
	}
	output = maskSecrets(output, extraEnv)

	// Track CWD change if policy allows and command was successful
	if ctx.CwdPolicy == tool.CwdPolicyTrack && exitCode == 0 && ctx.CwdManager != nil {
		// Try to detect if the command changed CWD
		// This is best-effort; a more robust solution would parse the command
		if isCdCommand(cmd) {
			// Extract target directory from cd command
			if newCwd := extractCdTarget(cmd, cwd); newCwd != "" {
				if err := ctx.CwdManager.Set(newCwd); err != nil {
					// Log but don't fail the command
					// The actual CWD change happened in the subshell anyway
				}
			}
		}
	}

	if exitCode != 0 {
		return fmt.Sprintf("❌ COMMAND FAILED (exit code: %d)\n%s", exitCode, output), nil
	}
	if output == "" {
		return "(no output)", nil
	}
	return output, nil
}

// determineCwd determines the working directory for command execution.
// Priority: explicit cwd arg > CwdManager > Workspace > current process CWD
func (m *Manager) determineCwd(ctx *tool.ToolContext, explicitCwd string) string {
	if explicitCwd != "" {
		return explicitCwd
	}

	if ctx.CwdManager != nil {
		return ctx.CwdManager.Get()
	}

	if ws := ctx.GetCwd(); ws != "" {
		return ws
	}

	// Fall back to current process CWD
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}

	return ""
}

// isCdCommand checks if a command is a cd (change directory) command.
func isCdCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	return strings.HasPrefix(trimmed, "cd ") || trimmed == "cd"
}

// extractCdTarget extracts the target directory from a cd command.
// Returns empty string if cannot determine.
func extractCdTarget(cmd, currentCwd string) string {
	trimmed := strings.TrimSpace(cmd)

	// Strip "cd" prefix
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "cd"))
	if rest == "" {
		// "cd" alone goes to home directory
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return ""
	}

	// "cd -" means previous directory; we can't track that
	if rest == "-" {
		return ""
	}

	// Handle quoted paths: "cd '/some path'" or 'cd "/some path"'
	target := rest
	if (strings.HasPrefix(target, "\"") && strings.HasSuffix(target, "\"")) ||
		(strings.HasPrefix(target, "'") && strings.HasSuffix(target, "'")) {
		target = target[1 : len(target)-1]
		if target == "" {
			return ""
		}
	} else {
		// Unquoted: take only the first field
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return ""
		}
		target = fields[0]
	}

	// Handle ~ expansion
	if target == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return ""
	}
	if strings.HasPrefix(target, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			target = filepath.Join(home, target[2:])
		}
	}

	// Resolve relative to current CWD
	if !filepath.IsAbs(target) {
		target = filepath.Join(currentCwd, target)
	}

	return filepath.Clean(target)
}

func (m *Manager) shellOutputTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "shellOutput",
			Description: "Read output from a background shell.",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "shell background job output read",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"shell_id": map[string]any{"type": "string", "description": "Shell ID"},
					"offset":   map[string]any{"type": "integer", "default": 0},
					"limit":    map[string]any{"type": "integer"},
				},
				"required": []string{"shell_id"},
			},
		},
		Handler: m.execOutputHandler,
	}
}

func (m *Manager) execOutputHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	shellID, _ := args["shell_id"].(string)
	s, ok := m.shellManager.Get(shellID)
	if !ok {
		return nil, core.MakeErrToolExec("shellOutput", fmt.Errorf("shell %q not found", shellID)).Build()
	}

	output := s.Output()
	offset := 0
	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
	}
	if offset > len(output) {
		offset = len(output)
	}

	end := len(output)
	if l, ok := args["limit"].(float64); ok && int(l) > 0 {
		if offset+int(l) < end {
			end = offset + int(l)
		}
	}

	chunk := output[offset:end]
	chunk = maskSecrets(chunk, s.masks)
	if chunk == "" {
		chunk = "(no output)"
	}

	rc := "null"
	if code := s.ReturnCode(); code != nil {
		rc = fmt.Sprintf("%d", *code)
	}

	return fmt.Sprintf("id: %s\nstatus: %s\nexit_code: %s\nnext_offset: %d\noutput:\n%s",
		s.ID, s.Status(), rc, end, chunk), nil
}

func (m *Manager) shellKillTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "shellKill",
			Description: "Terminate a background shell process.",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "shell background job kill terminate stop",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"shell_id": map[string]any{"type": "string", "description": "Shell ID"},
				},
				"required": []string{"shell_id"},
			},
		},
		Handler: m.execKillHandler,
	}
}

func (m *Manager) execKillHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	shellID, _ := args["shell_id"].(string)
	s, err := m.shellManager.Terminate(shellID)
	if err != nil {
		return nil, core.FromError(err).With("tool", "shellKill").Build()
	}
	rc := "null"
	if code := s.ReturnCode(); code != nil {
		rc = fmt.Sprintf("%d", *code)
	}
	return fmt.Sprintf("id: %s\nstatus: %s\nexit_code: %s", s.ID, s.Status(), rc), nil
}

func (m *Manager) shellJobsTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "shellJobs",
			Description: "List all background shell processes and their status.",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "shell background job list status",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler: m.shellJobsHandler,
	}
}

func (m *Manager) shellJobsHandler(_ *tool.ToolContext, _ map[string]any) (any, error) {
	shells := m.shellManager.List()
	if len(shells) == 0 {
		return "(no background jobs)", nil
	}
	var lines []string
	for _, s := range shells {
		rc := "null"
		if code := s.ReturnCode(); code != nil {
			rc = fmt.Sprintf("%d", *code)
		}
		lines = append(lines, fmt.Sprintf("- %s  status=%s  exit_code=%s", s.ID, s.Status(), rc))
	}
	return fmt.Sprintf("%d jobs:\n%s", len(lines), strings.Join(lines, "\n")), nil
}

func executeCommand(ctx context.Context, cmd, cwd string, timeout int, interactive bool, extraEnv map[string]string, tempCleanup []string) (string, int, error) {
	defer cleanupTempPaths(tempCleanup)

	shellBin := getShell()
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	args := shellArgs(interactive, cmd)
	command := exec.CommandContext(execCtx, shellBin, args...)
	command.Dir = cwd
	command.Env = mergeOSEnv(extraEnv)

	output, err := command.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", -1, err
		}
	}
	return string(output), exitCode, nil
}

func getShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

func shellArgs(interactive bool, cmd string) []string {
	if interactive {
		return []string{"-i", "-c", cmd}
	}
	return []string{"-c", cmd}
}
