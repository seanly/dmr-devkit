//go:build !windows

package script

import (
	"os/exec"
	"syscall"
)

// configureScriptExec puts the script in its own process group and arranges
// for timeout cancellation to kill the whole group. This is essential: a
// script often spawns children (e.g. `sleep`, `curl`) that the default
// per-process SIGKILL would orphan, leaving them running past the deadline.
func configureScriptExec(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative PID signals the entire process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
