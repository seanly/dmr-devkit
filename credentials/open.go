package credentials

import (
	"fmt"
	"path/filepath"
	"strings"
)

// OpenOpts configures OpenStore.
type OpenOpts struct {
	Driver                 string // mem | file (default file)
	Dir                    string // relative to ConfigBase, default "credentials"
	ConfigBase             string // absolute directory of config file
	AllowInsecurePlaintext bool
	DEK                    []byte         // pre-loaded DEK; if set, skips LoadDEKAuto
	RawConfig              map[string]any // driver-specific config (e.g. vault_addr)
}

// OpenStore builds a Store from driver name and options.
func OpenStore(opts OpenOpts) (Store, error) {
	driver := strings.TrimSpace(strings.ToLower(opts.Driver))
	if driver == "" {
		driver = "file"
	}
	base := strings.TrimSpace(opts.ConfigBase)
	if base == "" {
		return nil, fmt.Errorf("credentials: ConfigBase is required")
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	opts.ConfigBase = absBase

	factory, ok := drivers[driver]
	if !ok {
		return nil, fmt.Errorf("credentials: unsupported driver %q", driver)
	}
	return factory(opts)
}
