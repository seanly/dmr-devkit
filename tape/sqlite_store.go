package tape

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/seanly/dmr-devkit/config"
	_ "modernc.org/sqlite"
)

// SQLiteStoreConfig holds SQLite-specific configuration.
type SQLiteStoreConfig struct {
	EnableFTS5 config.FTS5Mode
}

// SQLiteTapeStore persists tape entries in a SQLite database.
type SQLiteTapeStore struct {
	mu                 sync.RWMutex
	db                 *sql.DB
	config             SQLiteStoreConfig
	useFTS5            atomic.Bool
	migrationCompleted atomic.Bool
}

// SQLDriverModernc is the default pure-Go SQLite driver (modernc.org/sqlite).
const SQLDriverModernc = "sqlite"

// NewSQLiteTapeStore opens (or creates) a SQLite database at dbPath using the modernc driver.
func NewSQLiteTapeStore(dbPath string, config SQLiteStoreConfig) (*SQLiteTapeStore, error) {
	return NewSQLiteTapeStoreWithDriver(dbPath, SQLDriverModernc, config)
}

// NewSQLiteTapeStoreWithDriver opens a tape store with the given database/sql driver name.
// Used by sqlcompat evaluation (e.g. driver "turso" via tursogo). For modernc, WAL query params are applied.
func NewSQLiteTapeStoreWithDriver(dbPath, driver string, config SQLiteStoreConfig) (*SQLiteTapeStore, error) {
	if driver == "" {
		driver = SQLDriverModernc
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := dbPath
	if driver == SQLDriverModernc {
		// Keep a single SQLite connection to serialize writes and avoid
		// SQLITE_BUSY caused by concurrent writers from the same process.
		// A generous busy timeout handles transient contention from other processes.
		dsn = dbPath + "?_journal_mode=WAL&_busy_timeout=30000"
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}

	// Serialize all SQLite access through one connection. SQLite only allows one
	 // writer at a time even in WAL mode; pooling multiple connections makes
	 // SQLITE_BUSY likely when goroutines append concurrently.
	if driver == SQLDriverModernc {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}

	store := &SQLiteTapeStore{
		db:     db,
		config: config,
	}

	if err := store.initBaseSchema(); err != nil {
		db.Close()
		return nil, err
	}

	enableFTS5 := store.parseFTS5Config()

	switch enableFTS5 {
	case "true":
		if err := store.initFTS5(); err != nil {
			db.Close()
			return nil, fmt.Errorf("init FTS5: %w", err)
		}
		if err := store.completeMigration(context.Background()); err != nil {
			slog.Error("FTS5 migration failed", "error", err)
			store.useFTS5.Store(false)
		} else {
			store.useFTS5.Store(true)
			store.migrationCompleted.Store(true)
			slog.Info("FTS5 enabled and ready")
		}

	case "auto":
		if err := store.initFTS5(); err != nil {
			slog.Warn("FTS5 init failed, using LIKE", "error", err)
			store.useFTS5.Store(false)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := store.migrateWithTimeout(ctx); err != nil {
				slog.Info("FTS5 migration will continue in background", "error", err)
				store.useFTS5.Store(true)
				store.migrationCompleted.Store(false)
				// Delay background migration to allow main init to complete
				go func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("FTS5 migration panic recovered", "recover", r)
						}
					}()
					// Wait a bit for the main initialization to finish and release locks
					time.Sleep(3 * time.Second)

					// Check if migration is still needed
					if !store.isMigrationNeeded() {
						store.migrationCompleted.Store(true)
						slog.Info("FTS5 migration already completed by another process")
						return
					}

					if err := store.completeMigrationWithRetry(context.Background(), 3); err != nil {
						slog.Error("FTS5 background migration failed after retries", "error", err)
						// Don't disable FTS5 - new entries still work via trigger
					} else {
						store.migrationCompleted.Store(true)
						slog.Info("FTS5 background migration completed")
					}
				}()
			} else {
				store.useFTS5.Store(true)
				store.migrationCompleted.Store(true)
				slog.Info("FTS5 enabled and ready")
			}
		}

	default:
		store.useFTS5.Store(false)
		slog.Info("FTS5 disabled, using LIKE")
	}

	return store, nil
}

func (s *SQLiteTapeStore) parseFTS5Config() string {
	return s.config.EnableFTS5.String()
}

func (s *SQLiteTapeStore) initBaseSchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS entries (id INTEGER PRIMARY KEY AUTOINCREMENT, tape TEXT NOT NULL, kind TEXT NOT NULL, payload TEXT NOT NULL DEFAULT '{}', meta TEXT NOT NULL DEFAULT '{}', date TEXT NOT NULL DEFAULT ''); CREATE INDEX IF NOT EXISTS idx_entries_tape ON entries(tape); CREATE INDEX IF NOT EXISTS idx_entries_tape_kind ON entries(tape, kind); CREATE INDEX IF NOT EXISTS idx_entries_date ON entries(date);`)
	return err
}

func (s *SQLiteTapeStore) initFTS5() error {
	_, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(content, content_rowid=id, tokenize='trigram');`)
	if err != nil {
		return fmt.Errorf("create fts5 table: %w", err)
	}

	_, err = s.db.Exec(`CREATE TRIGGER IF NOT EXISTS entries_fts_ai AFTER INSERT ON entries BEGIN INSERT INTO entries_fts(rowid, content) VALUES (new.id, COALESCE(new.payload, '') || ' ' || COALESCE(new.meta, '')); END;`)
	if err != nil {
		return fmt.Errorf("create insert trigger: %w", err)
	}

	_, err = s.db.Exec(`CREATE TRIGGER IF NOT EXISTS entries_fts_ad AFTER DELETE ON entries BEGIN INSERT INTO entries_fts(entries_fts, rowid, content) VALUES ('delete', old.id, COALESCE(old.payload, '') || ' ' || COALESCE(old.meta, '')); END;`)
	if err != nil {
		return fmt.Errorf("create delete trigger: %w", err)
	}

	return nil
}

func (s *SQLiteTapeStore) completeMigration(ctx context.Context) error {
	var unmigrated int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE id NOT IN (SELECT rowid FROM entries_fts)`).Scan(&unmigrated)
	if err != nil {
		return fmt.Errorf("check unmigrated count: %w", err)
	}
	if unmigrated == 0 {
		return nil
	}

	slog.Info("FTS5 migrating entries", "count", unmigrated)
	batchSize := 1000 // Smaller batch size to reduce lock contention
	totalMigrated := 0
	batchCount := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		result, err := s.db.Exec(`INSERT OR IGNORE INTO entries_fts(rowid, content) SELECT id, COALESCE(payload, '') || ' ' || COALESCE(meta, '') FROM entries WHERE id NOT IN (SELECT rowid FROM entries_fts) LIMIT ?`, batchSize)
		if err != nil {
			return fmt.Errorf("migrate batch: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			break
		}
		totalMigrated += int(rows)
		batchCount++

		// Log progress every 1000 entries
		if totalMigrated%1000 == 0 || int(rows) < batchSize {
			slog.Info("FTS5 migration progress", "migrated", totalMigrated, "total", unmigrated)
		}

		// Small delay every 10 batches to allow other operations
		if batchCount%10 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}

	s.db.Exec(`INSERT INTO entries_fts(entries_fts) VALUES('optimize')`)
	slog.Info("FTS5 migration completed", "count", totalMigrated)
	return nil
}

// completeMigrationWithRetry attempts migration with retries
func (s *SQLiteTapeStore) completeMigrationWithRetry(ctx context.Context, maxRetries int) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			slog.Info("FTS5 migration retry", "attempt", i+1, "maxRetries", maxRetries)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i) * time.Second):
			}
		}

		err = s.completeMigration(ctx)
		if err == nil {
			return nil
		}

		// Check if it's a lock error
		if !strings.Contains(err.Error(), "database is locked") {
			return err // Non-retryable error
		}

		slog.Warn("FTS5 migration attempt failed, will retry", "attempt", i+1, "error", err)
	}
	return fmt.Errorf("migration failed after %d retries: %w", maxRetries, err)
}

func (s *SQLiteTapeStore) migrateWithTimeout(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- s.completeMigration(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("migration timeout: %w", ctx.Err())
	}
}

// isMigrationNeeded checks if there are entries that need migration
func (s *SQLiteTapeStore) isMigrationNeeded() bool {
	var count int
	// Use a short timeout query
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id NOT IN (SELECT rowid FROM entries_fts) LIMIT 1`)
	err := row.Scan(&count)
	return err == nil && count > 0
}

func (s *SQLiteTapeStore) ListTapes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT DISTINCT tape FROM entries ORDER BY tape")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			names = append(names, name)
		}
	}
	return names
}

func (s *SQLiteTapeStore) Reset(tape string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec("DELETE FROM entries WHERE tape = ?", tape)
}

func (s *SQLiteTapeStore) Append(tape string, entry TapeEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payloadJSON, err := json.Marshal(entry.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	metaJSON, err := json.Marshal(entry.Meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if entry.Meta == nil {
		metaJSON = []byte("{}")
	}

	const maxRetries = 5
	var result sql.Result
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			// Exponential backoff: 20ms, 40ms, 80ms, 160ms
			time.Sleep(time.Duration(1<<(i-1)) * 20 * time.Millisecond)
		}
		result, err = s.db.Exec(
			"INSERT INTO entries (tape, kind, payload, meta, date) VALUES (?, ?, ?, ?, ?)",
			tape, entry.Kind, string(payloadJSON), string(metaJSON), entry.Date,
		)
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "database is locked") {
			return fmt.Errorf("insert entry: %w", err)
		}
		slog.Debug("sqlite tape: insert busy, retrying", "attempt", i+1, "error", err)
	}
	if err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}
	if id, idErr := result.LastInsertId(); idErr == nil {
		entry.ID = int(id)
	}
	return nil
}

func (s *SQLiteTapeStore) FetchAll(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if opts != nil && opts.AfterID > 0 {
		o := *opts
		o.LastAnchor = false
		o.AfterAnchor = ""
		o.BetweenAnchors = [2]string{}
		return s.fetchFiltered(tape, &o)
	}

	if opts != nil && opts.LastAnchor {
		return s.fetchLastAnchor(tape, opts)
	}
	if opts != nil && opts.AfterAnchor != "" {
		return s.fetchAfterAnchor(tape, opts)
	}
	if opts != nil && opts.BetweenAnchors != [2]string{} {
		return s.fetchBetweenAnchors(tape, opts)
	}

	return s.fetchFiltered(tape, opts)
}

func (s *SQLiteTapeStore) fetchFiltered(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	return s.fetchFilteredWithMode(tape, opts, "")
}

// FetchAllSearch is a SQLite-specific method that forces a search mode ("fts5" or "like").
// Use this in tests to verify FTS5 vs LIKE behavior without polluting the shared FetchOpts.
func (s *SQLiteTapeStore) FetchAllSearch(tape string, opts *FetchOpts, searchMode string) ([]TapeEntry, error) {
	return s.fetchFilteredWithMode(tape, opts, searchMode)
}

func (s *SQLiteTapeStore) fetchFilteredWithMode(tape string, opts *FetchOpts, forceMode string) ([]TapeEntry, error) {
	if forceMode != "" {
		switch forceMode {
		case "fts5":
			return s.fetchWithFTS5(tape, opts)
		case "like":
			return s.fetchWithLike(tape, opts)
		}
	}

	// Auto select based on FTS5 availability
	if s.useFTS5.Load() && opts != nil && opts.TextQuery != "" {
		entries, err := s.fetchWithFTS5(tape, opts)
		if err == nil {
			return entries, nil
		}
		// FTS5 failed, fallback to LIKE
		slog.Warn("FTS5 query failed, fallback to LIKE", "error", err)
	}

	return s.fetchWithLike(tape, opts)
}

func (s *SQLiteTapeStore) fetchWithLike(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	var where []string
	var args []any

	where = append(where, "tape = ?")
	args = append(args, tape)

	if opts != nil {
		if opts.AfterID > 0 {
			where = append(where, "id > ?")
			args = append(args, opts.AfterID)
		}
		if opts.StartDate != "" {
			where = append(where, "date >= ?")
			args = append(args, opts.StartDate)
		}
		if opts.EndDate != "" {
			where = append(where, "date <= ?")
			args = append(args, normEndDate(opts.EndDate))
		}
		if len(opts.Kinds) > 0 {
			placeholders := make([]string, len(opts.Kinds))
			for i, k := range opts.Kinds {
				placeholders[i] = "?"
				args = append(args, k)
			}
			where = append(where, "kind IN ("+strings.Join(placeholders, ",")+")")
		}
		if opts.TextQuery != "" {
			where = append(where, "(payload LIKE ? OR meta LIKE ?)")
			q := "%" + opts.TextQuery + "%"
			args = append(args, q, q)
		}
	}

	query := "SELECT id, kind, payload, meta, date FROM entries WHERE " + strings.Join(where, " AND ") + " ORDER BY id"
	if opts != nil && opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return s.queryEntries(query, args...)
}

func (s *SQLiteTapeStore) fetchWithFTS5(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	if opts == nil || opts.TextQuery == "" {
		return s.fetchWithLike(tape, opts)
	}

	matchQuery := buildFTS5MatchQuery(opts.TextQuery)

	// Query: Find entries in specific tape that match FTS content
	sql := `SELECT e.id, e.kind, e.payload, e.meta, e.date
		FROM entries e
		JOIN entries_fts f ON e.id = f.rowid
		WHERE e.tape = ? AND entries_fts MATCH ?`
	args := []any{tape, matchQuery}

	sql += " ORDER BY rank"

	if opts.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", opts.Limit+100)
	}

	entries, err := s.queryEntries(sql, args...)
	if err != nil {
		return nil, err
	}

	// Apply post-filters (date, kinds) - though most should be handled by SQL now
	return applyPostFilters(entries, opts), nil
}

// buildFTS5MatchQuery converts a user query into a safe FTS5 MATCH expression.
// It preserves boolean operators (AND, OR, NOT) and quotes tokens that contain
// characters not allowed in bare FTS5 tokens (e.g., '.', '-', '/', etc.).
func buildFTS5MatchQuery(query string) string {
	tokens := strings.Fields(query)
	for i, tok := range tokens {
		upper := strings.ToUpper(tok)
		if upper == "AND" || upper == "OR" || upper == "NOT" {
			continue
		}
		// Already explicitly quoted by the user
		if strings.HasPrefix(tok, `"`) && strings.HasSuffix(tok, `"`) {
			tokens[i] = `"` + strings.ReplaceAll(tok[1:len(tok)-1], `"`, `""`) + `"`
			continue
		}
		if isSafeFTS5Token(tok) {
			continue
		}
		tokens[i] = `"` + strings.ReplaceAll(tok, `"`, `""`) + `"`
	}
	return strings.Join(tokens, " ")
}

// isSafeFTS5Token reports whether a token can be passed unquoted to FTS5.
// Safe characters are ASCII letters, digits, underscore, and CJK characters.
func isSafeFTS5Token(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		// CJK Unified Ideographs, CJK Extensions A-D, and Kangxi Radicals
		if (r >= '\u4e00' && r <= '\u9fff') ||
			(r >= '\u3400' && r <= '\u4dbf') ||
			(r >= '\uf900' && r <= '\ufaff') ||
			(r >= '\U00020000' && r <= '\U0002a6df') ||
			(r >= '\U0002a700' && r <= '\U0002b73f') ||
			(r >= '\U0002b740' && r <= '\U0002b81f') ||
			(r >= '\U0002f800' && r <= '\U0002fa1f') {
			continue
		}
		return false
	}
	return true
}

func (s *SQLiteTapeStore) fetchLastAnchor(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	var anchorID int
	err := s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = ? AND kind = 'anchor' ORDER BY id DESC LIMIT 1",
		tape,
	).Scan(&anchorID)
	if err != nil {
		return nil, fmt.Errorf("no anchor found")
	}

	entries, err := s.queryEntries(
		"SELECT id, kind, payload, meta, date FROM entries WHERE tape = ? AND id > ? ORDER BY id",
		tape, anchorID,
	)
	if err != nil {
		return nil, err
	}
	return applyPostFilters(entries, opts), nil
}

func (s *SQLiteTapeStore) fetchAfterAnchor(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	var anchorID int
	err := s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = ? AND kind = 'anchor' AND json_extract(payload, '$.name') = ? ORDER BY id DESC LIMIT 1",
		tape, opts.AfterAnchor,
	).Scan(&anchorID)
	if err != nil {
		return nil, fmt.Errorf("anchor '%s' not found", opts.AfterAnchor)
	}

	entries, err := s.queryEntries(
		"SELECT id, kind, payload, meta, date FROM entries WHERE tape = ? AND id > ? ORDER BY id",
		tape, anchorID,
	)
	if err != nil {
		return nil, err
	}
	return applyPostFilters(entries, opts), nil
}

func (s *SQLiteTapeStore) fetchBetweenAnchors(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	var startID, endID int
	err := s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = ? AND kind = 'anchor' AND json_extract(payload, '$.name') = ? ORDER BY id DESC LIMIT 1",
		tape, opts.BetweenAnchors[0],
	).Scan(&startID)
	if err != nil {
		return nil, fmt.Errorf("anchor '%s' not found", opts.BetweenAnchors[0])
	}

	err = s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = ? AND kind = 'anchor' AND json_extract(payload, '$.name') = ? AND id > ? ORDER BY id ASC LIMIT 1",
		tape, opts.BetweenAnchors[1], startID,
	).Scan(&endID)
	if err != nil {
		return nil, fmt.Errorf("anchor '%s' not found", opts.BetweenAnchors[1])
	}

	entries, err := s.queryEntries(
		"SELECT id, kind, payload, meta, date FROM entries WHERE tape = ? AND id > ? AND id < ? ORDER BY id",
		tape, startID, endID,
	)
	if err != nil {
		return nil, err
	}
	return applyPostFilters(entries, opts), nil
}

func (s *SQLiteTapeStore) queryEntries(query string, args ...any) ([]TapeEntry, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []TapeEntry
	for rows.Next() {
		var e TapeEntry
		var payloadStr, metaStr string
		if err := rows.Scan(&e.ID, &e.Kind, &payloadStr, &metaStr, &e.Date); err != nil {
			slog.Error("sqlite tape: failed to scan row", "error", err)
			continue
		}
		if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
			slog.Error("sqlite tape: failed to unmarshal payload", "id", e.ID, "error", err)
		}
		if err := json.Unmarshal([]byte(metaStr), &e.Meta); err != nil {
			slog.Error("sqlite tape: failed to unmarshal meta", "id", e.ID, "error", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return entries, fmt.Errorf("row iteration: %w", err)
	}
	return entries, nil
}

// SetFTSEnabled enables or disables FTS5 at runtime.
func (s *SQLiteTapeStore) SetFTSEnabled(enabled bool) error {
	if enabled == s.useFTS5.Load() {
		return nil // No change
	}

	if enabled {
		// Check if FTS5 is ready
		s.mu.Lock()
		ready := s.isFTS5Ready()
		s.mu.Unlock()
		if !ready {
			return fmt.Errorf("FTS5 not ready, please complete migration first")
		}
		s.useFTS5.Store(true)
		slog.Info("FTS5 enabled at runtime")
	} else {
		s.useFTS5.Store(false)
		slog.Info("FTS5 disabled at runtime")
	}

	return nil
}

// IsFTSEnabled returns whether FTS5 is currently enabled.
func (s *SQLiteTapeStore) IsFTSEnabled() bool {
	return s.useFTS5.Load()
}

// IsFTS5Ready checks if FTS5 table exists and has data.
func (s *SQLiteTapeStore) isFTS5Ready() bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='entries_fts'").Scan(&count)
	return err == nil && count > 0
}

// RebuildFTS rebuilds the FTS5 index from scratch.
func (s *SQLiteTapeStore) RebuildFTS() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	slog.Info("FTS5 rebuilding index")

	// Clear existing index
	_, err := s.db.Exec("DELETE FROM entries_fts")
	if err != nil {
		return fmt.Errorf("clear fts5 index: %w", err)
	}

	// Rebuild from entries
	_, err = s.db.Exec(`
		INSERT INTO entries_fts(rowid, content)
		SELECT id, COALESCE(payload, '') || ' ' || COALESCE(meta, '')
		FROM entries
	`)
	if err != nil {
		return fmt.Errorf("rebuild fts5 index: %w", err)
	}

	// Optimize
	s.db.Exec("INSERT INTO entries_fts(entries_fts) VALUES('optimize')")

	slog.Info("FTS5 index rebuilt")
	return nil
}

// Close closes the database connection.
func (s *SQLiteTapeStore) Close() {
	s.db.Close()
}

// DB returns the underlying database handle (for sqlcompat evaluation and tests).
func (s *SQLiteTapeStore) DB() *sql.DB {
	return s.db
}

// applyPostFilters applies text query and kind filter for entries already sliced by anchor.
func applyPostFilters(entries []TapeEntry, opts *FetchOpts) []TapeEntry {
	if opts == nil {
		return entries
	}
	if opts.TextQuery != "" {
		q := strings.ToLower(opts.TextQuery)
		var filtered []TapeEntry
		for _, e := range entries {
			if matchText(e.Payload, q) || matchText(e.Meta, q) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	if len(opts.Kinds) > 0 {
		kindSet := make(map[string]bool, len(opts.Kinds))
		for _, k := range opts.Kinds {
			kindSet[k] = true
		}
		var filtered []TapeEntry
		for _, e := range entries {
			if kindSet[e.Kind] {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	if opts.Limit > 0 && len(entries) > opts.Limit {
		entries = entries[:opts.Limit]
	}
	return entries
}
