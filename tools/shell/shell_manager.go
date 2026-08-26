//go:build !windows

package shell

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type shellStatus string

const (
	shellRunning    shellStatus = "running"
	shellCompleted  shellStatus = "completed"
	shellTerminated shellStatus = "terminated"
)

const maxShellOutputBytes = 1 << 20 // 1 MB
const maxShellJobs = 100

type shell struct {
	ID         string
	cmd        *exec.Cmd
	output     strings.Builder
	mu         sync.Mutex
	status     shellStatus
	returnCode *int
	done       chan struct{}
	masks      map[string]string // injected credential values for output masking
}

type ShellManager struct {
	mu     sync.RWMutex
	shells map[string]*shell
	nextID int
}

func NewShellManager() *ShellManager {
	return &ShellManager{
		shells: make(map[string]*shell),
	}
}

func (m *ShellManager) Start(cmd string, cwd string, interactive bool, extraEnv map[string]string, tempCleanup []string) string {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("sh-%08d", m.nextID)

	// Enforce max concurrent shells by evicting the oldest completed job.
	if len(m.shells) >= maxShellJobs {
		var oldestID string
		var oldestTime int
		for sid, sh := range m.shells {
			if sh.status == shellRunning {
				continue
			}
			// Use a simple heuristic: lower ID = older job.
			num := 0
			fmt.Sscanf(sid, "sh-%08d", &num)
			if oldestID == "" || num < oldestTime {
				oldestID = sid
				oldestTime = num
			}
		}
		if oldestID != "" {
			delete(m.shells, oldestID)
		}
	}
	m.mu.Unlock()

	s := &shell{
		ID:     id,
		status: shellRunning,
		done:   make(chan struct{}),
		masks:  extraEnv,
	}

	shellBin := getShell()
	args := shellArgs(interactive, cmd)
	command := exec.Command(shellBin, args...)
	command.Dir = cwd
	command.Env = mergeOSEnv(extraEnv)
	s.cmd = command

	go func() {
		defer cleanupTempPaths(tempCleanup)
		defer close(s.done)
		defer func() {
			m.mu.Lock()
			delete(m.shells, id)
			m.mu.Unlock()
		}()
		out, err := command.CombinedOutput()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.appendOutput(out)
		code := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else {
				code = -1
			}
		}
		s.returnCode = &code
		if s.status == shellRunning {
			s.status = shellCompleted
		}
	}()

	m.mu.Lock()
	m.shells[id] = s
	m.mu.Unlock()

	return id
}

func (s *shell) appendOutput(p []byte) {
	remaining := maxShellOutputBytes - s.output.Len()
	if remaining <= 0 {
		return
	}
	if len(p) > remaining {
		p = p[len(p)-remaining:]
		s.output.Reset()
	}
	s.output.Write(p)
}

func (m *ShellManager) Get(id string) (*shell, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.shells[id]
	return s, ok
}

func (m *ShellManager) List() []*shell {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*shell, 0, len(m.shells))
	for _, s := range m.shells {
		out = append(out, s)
	}
	return out
}

func (m *ShellManager) Wait(id string) (*shell, error) {
	s, ok := m.Get(id)
	if !ok {
		return nil, fmt.Errorf("shell %q not found", id)
	}
	<-s.done
	return s, nil
}

func (m *ShellManager) Terminate(id string) (*shell, error) {
	s, ok := m.Get(id)
	if !ok {
		return nil, fmt.Errorf("shell %q not found", id)
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		// Process did not exit in time; continue anyway
	}
	s.mu.Lock()
	s.status = shellTerminated
	s.mu.Unlock()
	return s, nil
}

func (m *ShellManager) ShutdownAll() error {
	m.mu.RLock()
	ids := make([]string, 0, len(m.shells))
	for id := range m.shells {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		_, _ = m.Terminate(id)
	}
	return nil
}

func (s *shell) Output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.output.String()
}

func (s *shell) Status() shellStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *shell) ReturnCode() *int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.returnCode
}
