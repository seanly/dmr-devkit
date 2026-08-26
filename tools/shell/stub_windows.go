//go:build windows

package shell

import "github.com/seanly/dmr-devkit/tool"

// Manager is a no-op on Windows; use tools/powershell instead.
type Manager struct{}

// New returns an empty Manager on Windows.
func New(_ Options) *Manager { return &Manager{} }

// Tools returns no tools on Windows.
func (m *Manager) Tools() []*tool.Tool { return nil }

// Close is a no-op on Windows.
func (m *Manager) Close() error { return nil }
