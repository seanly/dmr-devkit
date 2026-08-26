//go:build !windows

package powershell

import "testing"

func TestNew_EmptyOnNonWindows(t *testing.T) {
	m := New(Options{Timeout: 30, UsePwsh: true})
	if tools := m.Tools(); len(tools) != 0 {
		t.Fatalf("expected no tools on non-Windows, got %d", len(tools))
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
