//go:build turso_eval

package sqlcompat

import (
	"database/sql"
	"fmt"
	"strings"
)

// TursoExperimentalDSN returns a DSN with experimental index methods enabled (required for USING fts).
func TursoExperimentalDSN(dbPath string) string {
	if strings.Contains(dbPath, "?") {
		return dbPath + "&experimental=index_method"
	}
	return dbPath + "?experimental=index_method"
}

// OpenTursoDB opens a local Turso database with experimental index methods enabled.
func OpenTursoDB(dbPath string) (*sql.DB, error) {
	return OpenDB(DriverTurso, TursoExperimentalDSN(dbPath))
}

// ProbeTursoNativeFTS checks whether the linked tursogo runtime supports CREATE INDEX ... USING fts.
func ProbeTursoNativeFTS(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS __turso_fts_probe (
		id INTEGER PRIMARY KEY,
		body TEXT
	)`); err != nil {
		return fmt.Errorf("probe table: %w", err)
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS __turso_fts_probe_idx`); err != nil {
		return fmt.Errorf("probe drop index: %w", err)
	}
	_, err := db.Exec(`CREATE INDEX __turso_fts_probe_idx ON __turso_fts_probe USING fts (body)`)
	if err != nil {
		return fmt.Errorf("probe fts index: %w", err)
	}
	return nil
}

// TursoNativeFTSAvailable reports whether native FTS (Tantivy) works in the current tursogo build.
func TursoNativeFTSAvailable(dbPath string) (bool, error) {
	db, err := OpenTursoDB(dbPath)
	if err != nil {
		return false, err
	}
	defer db.Close()
	if err := ProbeTursoNativeFTS(db); err != nil {
		return false, err
	}
	return true, nil
}
