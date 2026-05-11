package tape

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// validTSVectorLang restricts TSVectorLang to safe identifiers (lowercase letters and underscores only).
var validTSVectorLang = regexp.MustCompile(`^[a-z_]+$`)

// PGStoreConfig holds PostgreSQL-specific configuration.
type PGStoreConfig struct {
	EnableTSVector bool   // Enable full-text search with tsvector
	TSVectorLang   string // Language for tsvector (default: english)
}

// PGTapeStore persists tape entries in a PostgreSQL database.
type PGTapeStore struct {
	db     *sql.DB
	config PGStoreConfig
}

// NewPGTapeStore opens (or creates) a PostgreSQL connection.
func NewPGTapeStore(dsn string, config PGStoreConfig) (*PGTapeStore, error) {
	// Set default language to 'simple' for better multilingual support
	if config.TSVectorLang == "" {
		config.TSVectorLang = "simple"
	}

	// Validate TSVectorLang to prevent SQL injection — the value is interpolated
	// into SQL strings for to_tsvector/to_tsquery calls.
	if !validTSVectorLang.MatchString(config.TSVectorLang) {
		return nil, fmt.Errorf("invalid tsvector_lang %q: must contain only lowercase letters and underscores", config.TSVectorLang)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Set default application_name
	db.Exec("SET application_name = 'dmr'")

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	// Validate tsvector support if enabled
	if config.EnableTSVector {
		if err := validateTSVectorSupport(db, config.TSVectorLang); err != nil {
			db.Close()
			return nil, err
		}
	}

	// Initialize schema
	if err := initPGSchema(db, config); err != nil {
		db.Close()
		return nil, err
	}

	return &PGTapeStore{db: db, config: config}, nil
}

// validateTSVectorSupport checks if PostgreSQL supports tsvector and the specified language.
func validateTSVectorSupport(db *sql.DB, lang string) error {
	// Check PostgreSQL version (tsvector available since 8.3, released 2008)
	var version string
	err := db.QueryRow("SHOW server_version").Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to check PostgreSQL version: %w", err)
	}

	// Check if the specified text search configuration exists
	var exists bool
	err = db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM pg_ts_config WHERE cfgname = $1)",
		lang,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check text search configuration: %w", err)
	}

	if !exists {
		// List available configurations for helpful error message
		rows, _ := db.Query("SELECT cfgname FROM pg_ts_config ORDER BY cfgname")
		var available []string
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var cfg string
				if rows.Scan(&cfg) == nil {
					available = append(available, cfg)
				}
			}
		}

		return fmt.Errorf(
			"text search configuration '%s' not found in PostgreSQL.\n"+
				"Available configurations: %s\n"+
				"Hint: Use 'simple' for multilingual support, or install language-specific extensions (e.g., zhparser for Chinese)",
			lang, strings.Join(available, ", "),
		)
	}

	return nil
}

func initPGSchema(db *sql.DB, config PGStoreConfig) error {
	schemaSQL := `
		CREATE TABLE IF NOT EXISTS entries (
			id       SERIAL PRIMARY KEY,
			tape     TEXT    NOT NULL,
			kind     TEXT    NOT NULL,
			payload  JSONB   NOT NULL DEFAULT '{}',
			meta     JSONB   NOT NULL DEFAULT '{}',
			date     TEXT    NOT NULL DEFAULT ''`

	// Add tsvector column if enabled
	if config.EnableTSVector {
		schemaSQL += `,
			search_vector tsvector`
	}

	schemaSQL += `
		);
		CREATE INDEX IF NOT EXISTS idx_entries_tape ON entries(tape);
		CREATE INDEX IF NOT EXISTS idx_entries_tape_kind ON entries(tape, kind);
		CREATE INDEX IF NOT EXISTS idx_entries_date ON entries(date);`

	// Add GIN index for tsvector if enabled
	if config.EnableTSVector {
		schemaSQL += `
		CREATE INDEX IF NOT EXISTS idx_entries_search ON entries USING GIN(search_vector);`
	}

	_, err := db.Exec(schemaSQL)
	return err
}

func (s *PGTapeStore) ListTapes() []string {
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

func (s *PGTapeStore) Reset(tape string) {
	s.db.Exec("DELETE FROM entries WHERE tape = $1", tape)
}

func (s *PGTapeStore) Append(tape string, entry TapeEntry) error {
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

	var id int

	if s.config.EnableTSVector {
		// Insert with tsvector, use RETURNING id (pgx does not support LastInsertId)
		err = s.db.QueryRow(
			fmt.Sprintf(`INSERT INTO entries (tape, kind, payload, meta, date, search_vector)
			VALUES ($1, $2, $3, $4, $5, to_tsvector('%s', $3::text || ' ' || $4::text))
			RETURNING id`, s.config.TSVectorLang),
			tape, entry.Kind, string(payloadJSON), string(metaJSON), entry.Date,
		).Scan(&id)
	} else {
		// Insert without tsvector, use RETURNING id (pgx does not support LastInsertId)
		err = s.db.QueryRow(
			"INSERT INTO entries (tape, kind, payload, meta, date) VALUES ($1, $2, $3, $4, $5) RETURNING id",
			tape, entry.Kind, string(payloadJSON), string(metaJSON), entry.Date,
		).Scan(&id)
	}

	if err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}
	entry.ID = id
	return nil
}

func (s *PGTapeStore) FetchAll(tape string, opts *FetchOpts) ([]TapeEntry, error) {
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

func (s *PGTapeStore) fetchLastAnchor(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	// Find the last anchor ID
	var anchorID int
	err := s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = $1 AND kind = 'anchor' ORDER BY id DESC LIMIT 1",
		tape,
	).Scan(&anchorID)
	if err != nil {
		return nil, fmt.Errorf("no anchor found")
	}

	// Fetch entries after that anchor
	entries, err := s.queryEntries(
		"SELECT id, kind, payload, meta, date FROM entries WHERE tape = $1 AND id > $2 ORDER BY id",
		tape, anchorID,
	)
	if err != nil {
		return nil, err
	}
	return applyPostFilters(entries, opts), nil
}

func (s *PGTapeStore) fetchAfterAnchor(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	var anchorID int
	err := s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = $1 AND kind = 'anchor' AND payload->>'name' = $2 ORDER BY id DESC LIMIT 1",
		tape, opts.AfterAnchor,
	).Scan(&anchorID)
	if err != nil {
		return nil, fmt.Errorf("anchor '%s' not found", opts.AfterAnchor)
	}

	entries, err := s.queryEntries(
		"SELECT id, kind, payload, meta, date FROM entries WHERE tape = $1 AND id > $2 ORDER BY id",
		tape, anchorID,
	)
	if err != nil {
		return nil, err
	}
	return applyPostFilters(entries, opts), nil
}

func (s *PGTapeStore) fetchBetweenAnchors(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	var startID, endID int
	err := s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = $1 AND kind = 'anchor' AND payload->>'name' = $2 ORDER BY id DESC LIMIT 1",
		tape, opts.BetweenAnchors[0],
	).Scan(&startID)
	if err != nil {
		return nil, fmt.Errorf("anchor '%s' not found", opts.BetweenAnchors[0])
	}

	err = s.db.QueryRow(
		"SELECT id FROM entries WHERE tape = $1 AND kind = 'anchor' AND payload->>'name' = $2 AND id > $3 ORDER BY id ASC LIMIT 1",
		tape, opts.BetweenAnchors[1], startID,
	).Scan(&endID)
	if err != nil {
		return nil, fmt.Errorf("anchor '%s' not found", opts.BetweenAnchors[1])
	}

	entries, err := s.queryEntries(
		"SELECT id, kind, payload, meta, date FROM entries WHERE tape = $1 AND id > $2 AND id < $3 ORDER BY id",
		tape, startID, endID,
	)
	if err != nil {
		return nil, err
	}
	return applyPostFilters(entries, opts), nil
}

func (s *PGTapeStore) fetchFiltered(tape string, opts *FetchOpts) ([]TapeEntry, error) {
	var where []string
	var args []any

	where = append(where, "tape = $1")
	args = append(args, tape)
	argIdx := 2

	if opts != nil {
		if opts.AfterID > 0 {
			where = append(where, fmt.Sprintf("id > $%d", argIdx))
			args = append(args, opts.AfterID)
			argIdx++
		}
		if opts.StartDate != "" {
			where = append(where, fmt.Sprintf("date >= $%d", argIdx))
			args = append(args, opts.StartDate)
			argIdx++
		}
		if opts.EndDate != "" {
			where = append(where, fmt.Sprintf("date <= $%d", argIdx))
			args = append(args, normEndDate(opts.EndDate))
			argIdx++
		}
		if len(opts.Kinds) > 0 {
			placeholders := make([]string, len(opts.Kinds))
			for i, k := range opts.Kinds {
				placeholders[i] = fmt.Sprintf("$%d", argIdx)
				args = append(args, k)
				argIdx++
			}
			where = append(where, "kind IN ("+strings.Join(placeholders, ",")+")")
		}
		if opts.TextQuery != "" {
			if s.config.EnableTSVector {
				// Use tsvector full-text search
				where = append(where, fmt.Sprintf("search_vector @@ to_tsquery('%s', $%d)", s.config.TSVectorLang, argIdx))
				args = append(args, opts.TextQuery)
				argIdx++
			} else {
				// Fallback to LIKE search
				where = append(where, fmt.Sprintf("(payload::text LIKE $%d OR meta::text LIKE $%d)", argIdx, argIdx+1))
				q := "%" + opts.TextQuery + "%"
				args = append(args, q, q)
				argIdx += 2
			}
		}
	}

	query := "SELECT id, kind, payload, meta, date FROM entries WHERE " + strings.Join(where, " AND ") + " ORDER BY id"
	if opts != nil && opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return s.queryEntries(query, args...)
}

func (s *PGTapeStore) queryEntries(query string, args ...any) ([]TapeEntry, error) {
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
			continue
		}
		json.Unmarshal([]byte(payloadStr), &e.Payload)
		json.Unmarshal([]byte(metaStr), &e.Meta)
		entries = append(entries, e)
	}
	return entries, nil
}

// Close closes the database connection.
func (s *PGTapeStore) Close() {
	s.db.Close()
}
