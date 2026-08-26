//go:build windows

// Package powershell provides Windows PowerShell execution tools.
package powershell

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/seanly/dmr-devkit/tool"
)

// Manager holds background jobs and tool handlers.
type Manager struct {
	shellManager   *ShellManager
	defaultTimeout int
	usePwsh        bool
}

// New creates a Manager. Timeout defaults to 30 seconds when unset or non-positive.
func New(opts Options) *Manager {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	return &Manager{
		shellManager:   NewShellManager(),
		defaultTimeout: timeout,
		usePwsh:        opts.UsePwsh,
	}
}

// Tools returns the four PowerShell tools.
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
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "powershell",
			Description: "Execute a PowerShell command on Windows. Use background=true for long-running commands.",
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cmd":             map[string]any{"type": "string", "description": "PowerShell command to execute"},
					"cwd":             map[string]any{"type": "string", "description": "Working directory"},
					"timeout_seconds": map[string]any{"type": "integer", "default": 30},
					"background":      map[string]any{"type": "boolean", "default": false},
					"credential_bindings": map[string]any{
						"type":        "array",
						"description": "Bind stored credentials to environment variables. Use credentialsList/credentialsDescribe to discover available credentials and their fields.",
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
				"required": []string{"cmd"},
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

	if cwd == "" {
		if ws := ctx.GetCwd(); ws != "" {
			cwd = ws
		}
	}

	extraEnv, tempFiles := extractRuntimeInject(args)
	if background {
		shellID := m.shellManager.Start(cmd, cwd, m.usePwsh, extraEnv, tempFiles)
		return fmt.Sprintf("started: %s", shellID), nil
	}

	var baseCtx context.Context
	if ctx == nil || ctx.Ctx == nil {
		baseCtx = context.Background()
	} else {
		baseCtx = ctx.Ctx
	}

	output, exitCode, err := executeCommand(baseCtx, cmd, cwd, timeout, m.usePwsh, extraEnv, tempFiles)
	if err != nil {
		return nil, err
	}
	output = maskSecrets(output, extraEnv)
	if exitCode != 0 {
		return fmt.Sprintf("%s\n(exit code: %d)", output, exitCode), nil
	}
	if output == "" {
		return "(no output)", nil
	}
	return output, nil
}

func (m *Manager) shellOutputTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "powershellOutput",
			Description: "Read output from a background PowerShell process.",
			Group:       tool.ToolGroupExtended,
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
		return nil, fmt.Errorf("shell %q not found", shellID)
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
			Name:        "powershellKill",
			Description: "Terminate a background PowerShell process.",
			Group:       tool.ToolGroupExtended,
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
		return nil, err
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
			Name:        "powershellJobs",
			Description: "List all background PowerShell processes and their status.",
			Group:       tool.ToolGroupExtended,
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

func executeCommand(ctx context.Context, cmd, cwd string, timeout int, usePwsh bool, extraEnv map[string]string, tempCleanup []string) (string, int, error) {
	defer cleanupTempPaths(tempCleanup)

	pwshBin := getPowerShell(usePwsh)
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Use -EncodedCommand for better handling of special characters and security
	encoded := encodePowerShellCommand(cmd)
	args := []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", encoded}
	command := exec.CommandContext(execCtx, pwshBin, args...)
	command.Dir = cwd
	command.Env = mergeOSEnv(extraEnv)

	output, err := command.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			// Timeout error
			return string(output), -1, fmt.Errorf("command timed out after %d seconds", timeout)
		} else {
			// Other error (e.g., powershell.exe not found)
			return string(output), -1, fmt.Errorf("failed to execute PowerShell: %w", err)
		}
	}
	return string(output), exitCode, nil
}

func getPowerShell(usePwsh bool) string {
	if usePwsh {
		return "pwsh.exe"
	}
	return "powershell.exe"
}

func encodePowerShellCommand(cmd string) string {
	runes := []rune(cmd)
	encoded := utf16.Encode(runes)
	b := make([]byte, len(encoded)*2)
	for i, u := range encoded {
		b[i*2] = byte(u)
		b[i*2+1] = byte(u >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}
