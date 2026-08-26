//go:build !windows

package powershell

import "github.com/seanly/dmr-devkit/tool"

// Manager is a no-op on non-Windows; use tools/shell instead.
type Manager struct{}

// New returns an empty Manager on non-Windows.
func New(_ Options) *Manager { return &Manager{} }

// Tools returns no tools on non-Windows.
func (m *Manager) Tools() []*tool.Tool { return nil }

// Close is a no-op on non-Windows.
func (m *Manager) Close() error { return nil }
