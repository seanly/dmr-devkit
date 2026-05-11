package tape

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/seanly/dmr-devkit/config"
)

// StoreConfig holds configuration for creating a tape store.
type StoreConfig struct {
	Driver         string
	DSN            string
	Dir            string
	Workspace      string
	EnableTSVector bool            // PostgreSQL full-text search
	TSVectorLang   string          // tsvector language (default: english)
	EnableFTS5     config.FTS5Mode // SQLite FTS5: true/false/"auto" (default: false)
}

// NewStore creates a TapeStore based on the driver configuration.
func NewStore(cfg StoreConfig) (TapeStore, error) {
	switch cfg.Driver {
	case "mem", "memory":
		return NewInMemoryTapeStore(), nil
	case "file":
		dir := cfg.Dir
		if dir == "" {
			// V2 default: tapes stored under ~/.dmr/tape/
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".dmr", "tape")
		}
		return NewFileTapeStore(dir, cfg.Workspace)
	case "sqlite", "":
		dsn := cfg.DSN
		if dsn == "" {
			// V2 default: tapes.db stored under ~/.dmr/tape/
			home, _ := os.UserHomeDir()
			dsn = filepath.Join(home, ".dmr", "tape", "tapes.db")
		}
		return NewSQLiteTapeStore(dsn, SQLiteStoreConfig{
			EnableFTS5: cfg.EnableFTS5,
		})
	case "pg", "postgres":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("PostgreSQL DSN is required")
		}
		return NewPGTapeStore(cfg.DSN, PGStoreConfig{
			EnableTSVector: cfg.EnableTSVector,
			TSVectorLang:   cfg.TSVectorLang,
		})
	case "mysql":
		return nil, fmt.Errorf("MySQL tape store not implemented yet")
	default:
		return nil, fmt.Errorf("unknown tape driver: %s", cfg.Driver)
	}
}
