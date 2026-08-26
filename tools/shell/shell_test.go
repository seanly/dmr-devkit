//go:build !windows

package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/cwd"
	"github.com/seanly/dmr-devkit/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCdCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"cd", true},
		{"cd /tmp", true},
		{"cd ~", true},
		{"  cd /home  ", true},
		{"echo hello", false},
		{"cdr something", false},
		{"", false},
		{"CD /tmp", false}, // case-sensitive
	}
	for _, tt := range tests {
		if got := isCdCommand(tt.cmd); got != tt.want {
			t.Errorf("isCdCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestExecHandler_ErrorPrefix(t *testing.T) {
	m := New(Options{Interactive: true, Timeout: 30})
	ctx := &tool.ToolContext{
		Tape:       "test",
		CwdPolicy:  tool.CwdPolicyTrack,
		CwdManager: cwd.NewManager("/tmp", "/tmp"),
		Ctx:        context.Background(),
	}
	res, err := m.execHandler(ctx, map[string]any{"cmd": "false"})
	require.NoError(t, err)
	output := res.(string)
	assert.True(t, strings.HasPrefix(output, "❌ COMMAND FAILED (exit code: 1)"), "expected failure prefix, got: %q", output)
}

func TestExtractCdTarget(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name string
		cmd  string
		cwd  string
		want string
	}{
		{"bare cd", "cd", "/work", home},
		{"absolute", "cd /tmp", "/work", "/tmp"},
		{"relative", "cd sub", "/work", "/work/sub"},
		{"cd dash", "cd -", "/work", ""},
		{"tilde", "cd ~", "/work", home},
		{"tilde slash", "cd ~/docs", "/work", filepath.Join(home, "docs")},
		{"quoted", `cd "/some path"`, "/work", "/some path"},
		{"single quoted", "cd '/some path'", "/work", "/some path"},
		{"dotdot", "cd ..", "/work/sub", "/work"},
		{"empty quote", "cd ''", "/work", ""},
		{"empty double quote", `cd ""`, "/work", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCdTarget(tt.cmd, tt.cwd)
			if got != tt.want {
				t.Errorf("extractCdTarget(%q, %q) = %q, want %q", tt.cmd, tt.cwd, got, tt.want)
			}
		})
	}
}

func TestExecHandler_ContextCancelKillsProcess(t *testing.T) {
	m := New(Options{Interactive: true, Timeout: 30})
	runCtx, cancel := context.WithCancel(context.Background())
	toolCtx := &tool.ToolContext{
		Tape:       "test",
		CwdPolicy:  tool.CwdPolicyTrack,
		CwdManager: cwd.NewManager("/tmp", "/tmp"),
		Ctx:        runCtx,
	}

	done := make(chan struct{})
	var res any
	var err error
	go func() {
		res, err = m.execHandler(toolCtx, map[string]any{"cmd": "sleep 10", "timeout_seconds": 30.0})
		close(done)
	}()

	// Give the sleep process a moment to start, then cancel the job context.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop the shell process in time")
	}

	// A cancelled context should stop the process promptly. The shell may
	// return a non-zero exit code (often -1 because it was killed by a signal)
	// rather than an error value.
	if err == nil {
		output, ok := res.(string)
		if !ok || !strings.Contains(output, "❌ COMMAND FAILED") {
			t.Errorf("expected a failed command result after cancellation, got result=%v", res)
		}
	}
}
