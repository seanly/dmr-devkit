# CWD (Current Working Directory) Management

DMR now includes comprehensive CWD management to track, control, and recover working directory changes during agent execution.

## Features

1. **CWD State Tracking** - Track the current working directory across tool calls
2. **Directory Escape Prevention** - Prevent commands from escaping the workspace
3. **CWD Recovery** - Automatically recover when CWD is deleted
4. **Sandbox Mode** - Option to prevent any CWD changes

## Usage

### Basic Setup

```go
import "github.com/seanly/dmr-devkit/cwd"

// Initialize global CWD manager (call once at startup)
originalCwd, _ := os.Getwd()
cwd.InitGlobal(originalCwd, originalCwd)  // original, projectRoot

// Get current CWD
currentCwd := cwd.GetGlobalCwd()
```

### Tool Context with CWD Management

```go
import "github.com/seanly/dmr-devkit/tool"

ctx := tool.NewToolContext("tape123", "run456")
ctx.CwdManager = cwd.NewManager("/workspace", "/workspace")
ctx.CwdPolicy = tool.CwdPolicyTrack
ctx.CwdMustBeUnder = "/workspace"

// Get current CWD
cwd := ctx.GetCwd()

// Check if a directory is allowed
if err := ctx.CheckCwdAllowed("/outside/workspace"); err != nil {
    return err  // Directory escape detected!
}

// Recover if CWD was deleted
newCwd, recovered, err := ctx.RecoverCwd()
```

### CWD Policies

```go
// CwdPolicyAllow - Commands can change CWD freely (default)
ctx.CwdPolicy = tool.CwdPolicyAllow

// CwdPolicyTrack - Track CWD changes but allow them
ctx.CwdPolicy = tool.CwdPolicyTrack

// CwdPolicyPrevent - Prevent CWD changes (sandbox mode)
ctx.CwdPolicy = tool.CwdPolicyPrevent
```

### Shell Tool with CWD Management

The shell plugin automatically uses CWD management:

```toml
[[plugins]]
name = "shell"
enabled = true
```

When executing commands:

```bash
# CWD is automatically tracked
shell(cmd="cd subdir")
shell(cmd="pwd")  # Returns /workspace/subdir

# Directory escape is blocked (if CwdMustBeUnder is set)
shell(cmd="cd /etc")  # May require approval or be denied
```

## Policy Rules

The `default-v2.rego` policy includes CWD-related rules:

```rego
# Require approval for CWD escape attempts
decision := {"action": "require_approval", "reason": "cwd escapes workspace", "risk": "medium"} if {
    shell_cwd_escapes
}

# Deny cd commands that try to escape workspace
decision := {"action": "deny", "reason": "cd attempts to escape workspace", "risk": "medium"} if {
    shell_cd_escapes
}
```

## Implementation Details

### CWD Recovery

When a command deletes its own CWD (e.g., `rm -rf $(pwd)`), the CWD manager automatically recovers:

1. Check if current CWD exists
2. If not, try fallback directories in order:
   - Project root
   - Original CWD
   - Home directory
   - Root directory (/)
3. Return the first valid directory

### Directory Escape Detection

The `MustBeUnder` function checks if a path is contained within a base directory:

```go
manager := cwd.NewManager("/workspace", "")
err := manager.MustBeUnder("/etc/passwd", "/workspace")
// Returns: path "/etc/passwd" escapes base directory "/workspace"
```

## Configuration Example

```toml
[agent]
# Default CWD policy for all tools
# "allow" | "track" | "prevent"
cwd_policy = "track"

# Require all operations to stay under this directory
cwd_must_be_under = "/home/user/workspace"

[[plugins]]
name = "shell"
enabled = true
[plugins.config]
# Shell-specific CWD settings
prevent_cwd_changes = false  # If true, equivalent to CwdPolicyPrevent
```

## Migration Notes

For backward compatibility:
- If `CwdManager` is nil, tools fall back to `ctx.State["_runtime_workspace"]`
- If no workspace is set, tools use the current process CWD
- All CWD-related features are opt-in via configuration
