// Package plugin provides the core plugin system for DMR devkit.
//
// Plugins declare their capabilities via the Capabilities() method and implement
// the corresponding capability interfaces. The Registry collects plugins and indexes
// them by capability so callers can discover and invoke them efficiently.
//
// This replaces the older HookRegistry model (still available in dmr/pkg/plugin for
// backward compatibility) with a typed, interface-driven capability-registration model.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
)

// Plugin is the interface that all DMR plugins must implement.
// Plugins self-declare their capabilities via Capabilities().
type Plugin interface {
	Name() string
	Version() string
	Init(ctx context.Context, config map[string]any) error
	Shutdown(ctx context.Context) error
	Capabilities() []Capability
}

// HealthChecker is an optional interface that plugins can implement
// to report their health status.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// BindConfig converts a map[string]any plugin config into a typed struct.
// Uses JSON round-trip: marshal map to JSON, then unmarshal into target.
// Struct fields should use `json:"field_name"` tags for mapping.
func BindConfig[T any](config map[string]any, target *T) error {
	if len(config) == 0 {
		return nil
	}
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("bind config marshal: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("bind config unmarshal: %w", err)
	}
	return nil
}
