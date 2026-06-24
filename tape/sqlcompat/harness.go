// Package sqlcompat runs Turso compatibility evaluation against DMR tape SQL usage.
package sqlcompat

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Driver identifies a database/sql driver for evaluation.
type Driver string

const (
	DriverModernc Driver = "sqlite"
	DriverTurso   Driver = "turso"
)

// OpenDB opens a local database file with the given driver.
func OpenDB(driver Driver, dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	dsn := string(dbPath)
	if driver == DriverModernc {
		dsn = dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	}
	db, err := sql.Open(string(driver), dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}
	if driver == DriverModernc {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set wal: %w", err)
		}
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s: %w", driver, err)
	}
	return db, nil
}

// TursoEvalAvailable reports whether the turso_eval build tag and tursogo driver are linked.
func TursoEvalAvailable() bool {
	return tursoDriverRegistered()
}
