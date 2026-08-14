// Package search provides a persistent SQLite FTS5 full-text index over an OKF
// knowledge bundle's concepts.
//
// The index is a derived cache: the on-disk markdown files remain the single
// source of truth. On open the index is reconciled against the loaded bundle
// (cheap per-file mtime checks, hashing only changed files), and on every
// concept mutation the owning Service updates the index incrementally inside
// its write lock. A crash or external edit leaves the index stale until the
// next reconcile, which always re-converges to the markdown source.
//
// The trigram tokenizer is used so that mixed Chinese/English content matches
// by substring without a dictionary — close to the prior strings.Contains
// behavior. trigram requires query tokens of at least 3 Unicode codepoints;
// shorter queries surface ErrQueryTooShort so the caller can fall back to an
// in-memory scan.
package search

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/seanly/dmr-devkit/okf/bundle"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (CGO-free, FTS5 built in)
)

// ErrQueryTooShort signals that the query contains a token shorter than 3
// Unicode codepoints, which the trigram tokenizer cannot match. Callers should
// fall back to a substring scan.
var ErrQueryTooShort = errors.New("search: query token too short for trigram index")

// driverName is the database/sql driver name for modernc.org/sqlite.
const driverName = "sqlite"

// Hit is a single ranked search result.
type Hit struct {
	ID    string
	Score float64 // bm25 score (lower is better)
}

// Index is a persistent FTS5 full-text index over OKF concepts.
type Index struct {
	mu sync.Mutex
	db *sql.DB

	// Prepared statements are not used because modernc/sqlite + a single
	// connection handles ad-hoc Exec/Query fine; keeping the code simple.
}

// Open opens (or creates) the index database at dbPath, creates its schema if
// needed, and reconciles the index against the given concepts. dbPath's parent
// directory is created if missing. A single SQLite connection is used to
// serialize writes and avoid SQLITE_BUSY.
func Open(dbPath string, concepts map[string]*bundle.Concept) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("search: create db dir: %w", err)
	}

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=30000"
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("search: open %s: %w", driverName, err)
	}
	// Serialize all SQLite access through one connection. SQLite allows only
	// one writer at a time even in WAL mode; pooling makes SQLITE_BUSY likely.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	idx := &Index{db: db}
	if err := idx.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	if err := idx.Reconcile(concepts); err != nil {
		db.Close()
		return nil, err
	}
	return idx, nil
}

// initSchema creates the FTS5 virtual table and the metadata side table.
func (i *Index) initSchema() error {
	_, err := i.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS concepts_fts USING fts5(
			id UNINDEXED, title, type, description, tags, body,
			tokenize='trigram'
		);
		CREATE TABLE IF NOT EXISTS concepts_meta(
			id    TEXT PRIMARY KEY,
			rowid INTEGER NOT NULL,
			mtime INTEGER NOT NULL,
			hash  TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("search: init schema: %w", err)
	}
	return nil
}

// Reconcile brings the index in sync with the given concept set. For each
// concept it stats the file: if the stored mtime matches the file's current
// mtime the row is left untouched (the common no-change path — one cheap stat,
// no hashing). Otherwise the content hash is computed and the FTS row is
// upserted only if the hash changed. Rows whose concept no longer exists in
// the bundle are removed. All work happens in one transaction.
func (i *Index) Reconcile(concepts map[string]*bundle.Concept) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("search: reconcile begin: %w", err)
	}
	defer tx.Rollback() // safe to call after Commit (no-op)

	// Load existing metadata.
	type metaRow struct {
		rowid int64
		mtime int64
		hash  string
	}
	existing := make(map[string]metaRow, len(concepts))
	rows, err := tx.Query(`SELECT id, rowid, mtime, hash FROM concepts_meta`)
	if err != nil {
		return fmt.Errorf("search: reconcile read meta: %w", err)
	}
	for rows.Next() {
		var id string
		var m metaRow
		if err := rows.Scan(&id, &m.rowid, &m.mtime, &m.hash); err != nil {
			rows.Close()
			return fmt.Errorf("search: reconcile scan meta: %w", err)
		}
		existing[id] = m
	}
	rows.Close()

	// Upsert changed concepts.
	for id, c := range concepts {
		mtime, ok := fileMtime(c.FilePath)
		if !ok {
			continue // file missing / unreadable; skip rather than fail
		}
		if prev, found := existing[id]; found && prev.mtime == mtime {
			delete(existing, id) // unchanged; don't touch
			continue
		}
		hash := conceptHash(c)
		if prev, found := existing[id]; found && prev.hash == hash {
			// Content unchanged despite mtime change (e.g. touch); refresh mtime only.
			if _, err := tx.Exec(`UPDATE concepts_meta SET mtime = ? WHERE id = ?`, mtime, id); err != nil {
				return fmt.Errorf("search: reconcile update mtime %s: %w", id, err)
			}
			delete(existing, id)
			continue
		}
		if err := upsertLocked(tx, id, c, mtime, hash); err != nil {
			return err
		}
		delete(existing, id)
	}

	// Remove rows for concepts no longer in the bundle.
	for id, m := range existing {
		if _, err := tx.Exec(`DELETE FROM concepts_fts WHERE rowid = ?`, m.rowid); err != nil {
			return fmt.Errorf("search: reconcile delete fts %s: %w", id, err)
		}
		if _, err := tx.Exec(`DELETE FROM concepts_meta WHERE id = ?`, id); err != nil {
			return fmt.Errorf("search: reconcile delete meta %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: reconcile commit: %w", err)
	}
	return nil
}

// Upsert inserts or replaces a single concept's index row. Called by the
// Service after a successful concept write, inside the Service write lock.
func (i *Index) Upsert(c *bundle.Concept) error {
	if c == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	mtime, ok := fileMtime(c.FilePath)
	if !ok {
		// File not statable (shouldn't happen right after a write); hash anyway.
		mtime = 0
	}
	hash := conceptHash(c)

	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("search: upsert begin: %w", err)
	}
	defer tx.Rollback()
	if err := upsertLocked(tx, c.ID, c, mtime, hash); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertLocked deletes any prior FTS row for id (by stored rowid) and inserts
// a fresh one, recording the new rowid + mtime + hash in concepts_meta. The
// caller holds the index mutex and provides an open transaction.
func upsertLocked(tx *sql.Tx, id string, c *bundle.Concept, mtime int64, hash string) error {
	// Remove the previous FTS row if one exists.
	var prevRowid sql.NullInt64
	err := tx.QueryRow(`SELECT rowid FROM concepts_meta WHERE id = ?`, id).Scan(&prevRowid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("search: lookup meta %s: %w", id, err)
	}
	if prevRowid.Valid {
		if _, err := tx.Exec(`DELETE FROM concepts_fts WHERE rowid = ?`, prevRowid.Int64); err != nil {
			return fmt.Errorf("search: delete old fts %s: %w", id, err)
		}
	}

	title, typ, desc, tags, body := indexFields(c)
	res, err := tx.Exec(
		`INSERT INTO concepts_fts(id, title, type, description, tags, body) VALUES (?, ?, ?, ?, ?, ?)`,
		id, title, typ, desc, tags, body,
	)
	if err != nil {
		return fmt.Errorf("search: insert fts %s: %w", id, err)
	}
	newRowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("search: last rowid %s: %w", id, err)
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO concepts_meta(id, rowid, mtime, hash) VALUES (?, ?, ?, ?)`,
		id, newRowid, mtime, hash,
	); err != nil {
		return fmt.Errorf("search: upsert meta %s: %w", id, err)
	}
	return nil
}

// Remove deletes a single concept's index row. Called by the Service after a
// successful concept delete, inside the Service write lock. Missing rows are
// not an error.
func (i *Index) Remove(id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("search: remove begin: %w", err)
	}
	defer tx.Rollback()

	var rowid sql.NullInt64
	err = tx.QueryRow(`SELECT rowid FROM concepts_meta WHERE id = ?`, id).Scan(&rowid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // nothing to remove
	}
	if err != nil {
		return fmt.Errorf("search: remove lookup %s: %w", id, err)
	}
	if rowid.Valid {
		if _, err := tx.Exec(`DELETE FROM concepts_fts WHERE rowid = ?`, rowid.Int64); err != nil {
			return fmt.Errorf("search: remove fts %s: %w", id, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM concepts_meta WHERE id = ?`, id); err != nil {
		return fmt.Errorf("search: remove meta %s: %w", id, err)
	}
	return tx.Commit()
}

// Search runs a full-text query and returns ranked hits. Each whitespace-
// separated token in the query is treated as a required substring (implicit
// AND). Results are ranked by bm25 (lower score = better match) with column
// weights mirroring the prior hand-rolled scoring: title 10, type 8,
// description 5, tags 4, body 2.
//
// If any token is shorter than 3 Unicode codepoints, ErrQueryTooShort is
// returned so the caller can fall back to a substring scan.
func (i *Index) Search(query string, limit int) ([]Hit, error) {
	expr, err := buildMatchExpr(query)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	// bm25 weights map to columns in declared order: id, title, type,
	// description, tags, body. id is UNINDEXED (weight 0).
	rows, err := i.db.Query(
		`SELECT id, bm25(concepts_fts, 0.0, 10.0, 8.0, 5.0, 4.0, 2.0) AS rank
		 FROM concepts_fts
		 WHERE concepts_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		expr, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Score); err != nil {
			return nil, fmt.Errorf("search: scan: %w", err)
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// Close releases the database handle.
func (i *Index) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.db == nil {
		return nil
	}
	err := i.db.Close()
	i.db = nil
	return err
}

// buildMatchExpr turns a user query into a safe FTS5 MATCH expression: each
// whitespace-separated token is wrapped as a double-quoted string literal
// (internal quotes escaped by doubling) and joined with spaces (implicit
// AND). Returns ErrQueryTooShort if any token has fewer than 3 codepoints.
func buildMatchExpr(query string) (string, error) {
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return "", ErrQueryTooShort
	}
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if utf8.RuneCountInString(t) < 3 {
			return "", ErrQueryTooShort
		}
		escaped := strings.ReplaceAll(t, `"`, `""`)
		parts = append(parts, `"`+escaped+`"`)
	}
	return strings.Join(parts, " "), nil
}

// indexFields extracts the indexable text columns from a concept.
func indexFields(c *bundle.Concept) (title, typ, desc, tags, body string) {
	body = c.Body
	if c.Meta != nil {
		title = c.Meta.Title
		typ = c.Meta.Type
		desc = c.Meta.Description
		tags = strings.Join(c.Meta.Tags, " ")
	}
	return
}

// conceptHash returns a SHA-256 hex digest over the canonical concatenation of
// the indexable fields. Used to decide whether content has actually changed
// when an mtime change is observed.
func conceptHash(c *bundle.Concept) string {
	title, typ, desc, tags, body := indexFields(c)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s", title, typ, desc, tags, body)
	return hex.EncodeToString(h.Sum(nil))
}

// fileMtime returns the file's modification time as unix nanoseconds. The bool
// is false if the file could not be stat'd. Nanosecond resolution avoids
// missing sub-second edits during startup reconciliation.
func fileMtime(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return info.ModTime().UnixNano(), true
}
