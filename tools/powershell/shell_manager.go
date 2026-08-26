//go:build windows

package powershell

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

func (m *ShellManager) Start(cmd string, cwd string, usePwsh bool, extraEnv map[string]string, tempCleanup []string) string {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("ps-%08d", m.nextID)
	m.mu.Unlock()

	s := &shell{
		ID:     id,
		status: shellRunning,
		done:   make(chan struct{}),
		masks:  extraEnv,
	}

	pwshBin := getPowerShell(usePwsh)
	encoded := encodePowerShellCommand(cmd)
	args := []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", encoded}
	command := exec.Command(pwshBin, args...)
	command.Dir = cwd
	command.Env = mergeOSEnv(extraEnv)
	s.cmd = command

	go func() {
		defer cleanupTempPaths(tempCleanup)
		defer close(s.done)
		out, err := command.CombinedOutput()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.output.Write(out)
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
