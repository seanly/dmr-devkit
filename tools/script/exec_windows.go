//go:build windows

package script

import "os/exec"

// configureScriptExec is a no-op on Windows: the default CommandContext
// cancellation already kills the process, and POSIX process-group semantics
// do not apply. Script tools are primarily a Unix feature.
func configureScriptExec(cmd *exec.Cmd) {}
