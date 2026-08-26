package memory

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLiteBackend implements Backend with SQLite + FTS5 + Markdown Hybrid.
type SQLiteBackend struct {
	db                *sql.DB
	hasFTS5           bool
	hasAttachmentFTS5 bool
	dataSourceName    string // basename of DB file, for MemoryStats
	blob              AttachmentBlobStore
	attachmentKind    string             // "local" | "s3"
	mdStore           *markdownPageStore // markdown file layer (source of truth for content)
}

// NewSQLiteBackend creates a new SQLite-backed store.
func NewSQLiteBackend(cfg Config, blob AttachmentBlobStore) (*SQLiteBackend, error) {
	if dir := filepath.Dir(cfg.SQLiteDSN); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("mkdir for sqlite: %w", err)
		}
	}

	db, err := sql.Open("sqlite", cfg.SQLiteDSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// WAL mode for concurrent reads
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	// Foreign keys for CASCADE
	_, _ = db.Exec("PRAGMA foreign_keys=ON")

	kind := strings.ToLower(strings.TrimSpace(cfg.AttachmentBackend))
	switch kind {
	case "s3", "minio":
		kind = "s3"
	case "local", "fs", "file", "":
		kind = "local"
	default:
		kind = "local"
	}
	b := &SQLiteBackend{
		db:             db,
		dataSourceName: filepath.Base(cfg.SQLiteDSN),
		blob:           blob,
		attachmentKind: kind,
	}

	if err := b.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	if cfg.EnableFTS5 {
		if err := b.initFTS5(); err == nil {
			b.hasFTS5 = true
		}
	}

	// Initialize markdown export/backup layer if configured.
	if cfg.ExportDir != "" {
		b.mdStore = newMarkdownPageStore(cfg.ExportDir)
		// Migrate existing DB pages to markdown on first init.
		_ = b.mdStore.MigrateFromDB(b)
		log.Printf("[memory] markdown export enabled dir=%s", cfg.ExportDir)
	}

	return b, nil
}

func (b *SQLiteBackend) initSchema() error {
	_, err := b.db.Exec(`
		CREATE TABLE IF NOT EXISTS memory_pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL DEFAULT 'note',
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			frontmatter TEXT NOT NULL DEFAULT '{}',
			content_hash TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memory_pages_type ON memory_pages(type);

		CREATE TABLE IF NOT EXISTS memory_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			tag TEXT NOT NULL,
			UNIQUE(page_id, tag)
		);
		CREATE INDEX IF NOT EXISTS idx_memory_tags_tag ON memory_tags(tag);
		CREATE INDEX IF NOT EXISTS idx_memory_tags_page ON memory_tags(page_id);

		CREATE TABLE IF NOT EXISTS memory_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			from_page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			to_page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			link_type TEXT NOT NULL DEFAULT 'mentions',
			context TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(from_page_id, to_page_id, link_type)
		);
		CREATE INDEX IF NOT EXISTS idx_memory_links_from ON memory_links(from_page_id);
		CREATE INDEX IF NOT EXISTS idx_memory_links_to ON memory_links(to_page_id);

		CREATE TABLE IF NOT EXISTS memory_timeline (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			event_date TEXT NOT NULL,
			summary TEXT NOT NULL,
			UNIQUE(page_id, event_date, summary)
		);
		CREATE INDEX IF NOT EXISTS idx_memory_timeline_page ON memory_timeline(page_id);
		CREATE INDEX IF NOT EXISTS idx_memory_timeline_date ON memory_timeline(event_date);

		CREATE TABLE IF NOT EXISTS memory_page_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			type TEXT NOT NULL DEFAULT 'note',
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			frontmatter TEXT NOT NULL DEFAULT '{}',
			content_hash TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memory_revisions_page ON memory_page_revisions(page_id, created_at);

		CREATE TABLE IF NOT EXISTS memory_attachments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			public_id TEXT NOT NULL UNIQUE,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			mime TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			storage_kind TEXT NOT NULL DEFAULT 'local',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memory_attachments_page ON memory_attachments(page_id);
	`)
	return err
}

func (b *SQLiteBackend) initFTS5() error {
	if _, err := b.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS memory_pages_fts USING fts5(
			title, content
		);
	`); err != nil {
		return err
	}
	_, err := b.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS memory_attachments_fts USING fts5(
			name, summary
		);
	`)
	if err != nil {
		return err
	}
	b.hasAttachmentFTS5 = true
	return nil
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// pageID resolves a slug to its internal ID. Returns 0 if not found.
func (b *SQLiteBackend) pageID(slug string) (int, error) {
	var id int
	err := b.db.QueryRow(`SELECT id FROM memory_pages WHERE slug = ?`, slug).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (b *SQLiteBackend) PutPage(slug string, input PageInput) (Page, error) {
	h := contentHash(input.Content)
	fm := input.Frontmatter
	if fm == "" {
		fm = "{}"
	}
	typ := input.Type
	if typ == "" {
		typ = "note"
	}

	existingID, err := b.pageID(slug)
	if err != nil {
		return Page{}, err
	}

	if existingID > 0 {
		var oType, oTitle, oContent, oFM, oHash string
		err = b.db.QueryRow(`
			SELECT type, title, content, frontmatter, COALESCE(content_hash, '')
			FROM memory_pages WHERE id=?`, existingID,
		).Scan(&oType, &oTitle, &oContent, &oFM, &oHash)
		if err != nil {
			return Page{}, err
		}
		if oType != typ || oTitle != input.Title || oContent != input.Content || oFM != fm {
			if err := b.insertRevision(existingID, oType, oTitle, oContent, oFM, oHash); err != nil {
				return Page{}, err
			}
		}
		_, err = b.db.Exec(`
			UPDATE memory_pages SET type=?, title=?, content=?, frontmatter=?, content_hash=?, updated_at=CURRENT_TIMESTAMP
			WHERE id=?`,
			typ, input.Title, input.Content, fm, h, existingID,
		)
		if err != nil {
			return Page{}, err
		}
		if b.hasFTS5 {
			_, _ = b.db.Exec(`DELETE FROM memory_pages_fts WHERE rowid = ?`, existingID)
			_, _ = b.db.Exec(`INSERT INTO memory_pages_fts(rowid, title, content) VALUES (?, ?, ?)`, existingID, input.Title, input.Content)
		}
	} else {
		res, err := b.db.Exec(`
			INSERT INTO memory_pages (slug, type, title, content, frontmatter, content_hash)
			VALUES (?, ?, ?, ?, ?, ?)`,
			slug, typ, input.Title, input.Content, fm, h,
		)
		if err != nil {
			return Page{}, err
		}
		id, _ := res.LastInsertId()
		existingID = int(id)
		if b.hasFTS5 {
			_, _ = b.db.Exec(`INSERT INTO memory_pages_fts(rowid, title, content) VALUES (?, ?, ?)`, existingID, input.Title, input.Content)
		}
	}

	// Set tags if provided
	if len(input.Tags) > 0 {
		if err := b.setTags(existingID, input.Tags); err != nil {
			return Page{}, err
		}
	}

	p, err := b.GetPage(slug)
	if err != nil {
		return Page{}, err
	}
	if p == nil {
		return Page{}, fmt.Errorf("page %q not found after upsert", slug)
	}

	// Write to markdown file (hybrid storage layer).
	if b.mdStore != nil {
		_ = b.mdStore.WritePage(*p)
	}

	return *p, nil
}

func (b *SQLiteBackend) GetPage(slug string) (*Page, error) {
	p := &Page{}
	err := b.db.QueryRow(`
		SELECT id, slug, type, title, content, frontmatter, content_hash, created_at, updated_at
		FROM memory_pages WHERE slug = ?`, slug,
	).Scan(&p.ID, &p.Slug, &p.Type, &p.Title, &p.Content, &p.Frontmatter, &p.ContentHash, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// markdown_dir is a read-only backup/export. We do NOT read back from files;
	// users who want file-as-source-of-truth should use the llmwiki plugin.
	if len(p.Tags) == 0 {
		p.Tags, _ = b.GetTags(slug)
	}
	p.Timeline, _ = b.GetTimeline(slug)
	return p, nil
}

func (b *SQLiteBackend) DeletePage(slug string) error {
	pid, _ := b.pageID(slug)
	if pid == 0 {
		return fmt.Errorf("page %q not found", slug)
	}
	if err := b.deleteAttachmentBlobsForPage(pid); err != nil {
		return err
	}
	if b.hasFTS5 {
		_, _ = b.db.Exec(`DELETE FROM memory_pages_fts WHERE rowid = ?`, pid)
	}
	res, err := b.db.Exec(`DELETE FROM memory_pages WHERE slug = ?`, slug)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("page %q not found", slug)
	}
	// Delete markdown file (hybrid storage layer).
	if b.mdStore != nil {
		_ = b.mdStore.DeletePage(slug)
	}
	return nil
}

func (b *SQLiteBackend) ListPages(opts ListOpts) ([]Page, error) {
	q := "SELECT id, slug, type, title, content, frontmatter, content_hash, created_at, updated_at FROM memory_pages WHERE 1=1"
	var args []any

	if opts.Tag != "" {
		q += " AND id IN (SELECT page_id FROM memory_tags WHERE tag = ?)"
		args = append(args, opts.Tag)
	}
	if opts.SlugPrefix != "" {
		q += " AND slug LIKE ?"
		args = append(args, opts.SlugPrefix+"%")
	}
	if opts.Type != "" {
		q += " AND type = ?"
		args = append(args, opts.Type)
	}

	q += " ORDER BY updated_at DESC"

	needsFmFilter := len(opts.FrontmatterFilter) > 0

	if !needsFmFilter {
		limit := opts.Limit
		if limit <= 0 {
			limit = 20
		}
		q += " LIMIT ?"
		args = append(args, limit)

		if opts.Offset > 0 {
			q += " OFFSET ?"
			args = append(args, opts.Offset)
		}
	}

	rows, err := b.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.Slug, &p.Type, &p.Title, &p.Content, &p.Frontmatter, &p.ContentHash, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if needsFmFilter {
		pages = filterPagesByFrontmatter(pages, opts.FrontmatterFilter)
		pages = applyPagination(pages, opts.Limit, opts.Offset)
	}

	return pages, nil
}

func (b *SQLiteBackend) SearchPages(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}

	if b.hasFTS5 {
		results, err := b.searchFTS5(query, limit)
		if err == nil {
			return results, nil
		}
	}
	return b.searchLike(query, limit)
}

func (b *SQLiteBackend) searchFTS5(query string, limit int) ([]SearchResult, error) {
	rows, err := b.db.Query(`
		SELECT p.slug, p.title, p.type, snippet(memory_pages_fts, 1, '<<', '>>', '...', 32) as snippet, f.rank
		FROM memory_pages_fts f
		JOIN memory_pages p ON p.id = f.rowid
		WHERE f.title MATCH ? OR f.content MATCH ?
		ORDER BY f.rank
		LIMIT ?`,
		query, query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchResults(rows)
}

func (b *SQLiteBackend) searchLike(query string, limit int) ([]SearchResult, error) {
	pattern := "%" + query + "%"
	rows, err := b.db.Query(`
		SELECT slug, title, type, substr(content, 1, 200) as snippet, 0.0
		FROM memory_pages
		WHERE title LIKE ? OR content LIKE ?
		ORDER BY updated_at DESC
		LIMIT ?`,
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchResults(rows)
}

func scanSearchResults(rows *sql.Rows) ([]SearchResult, error) {
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Slug, &r.Title, &r.Type, &r.Snippet, &r.Rank); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// --- Tags ---

func (b *SQLiteBackend) setTags(pageID int, tags []string) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM memory_tags WHERE page_id = ?`, pageID)
	if err != nil {
		return err
	}
	for _, t := range tags {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		_, err = tx.Exec(`INSERT OR IGNORE INTO memory_tags (page_id, tag) VALUES (?, ?)`, pageID, t)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *SQLiteBackend) AddTags(slug string, tags []string) error {
	pid, err := b.pageID(slug)
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("page %q not found", slug)
	}

	// Get existing tags and merge
	existing, err := b.GetTags(slug)
	if err != nil {
		return err
	}
	existingSet := make(map[string]bool, len(existing))
	for _, t := range existing {
		existingSet[t] = true
	}
	merged := make([]string, len(existing))
	copy(merged, existing)
	for _, t := range tags {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" && !existingSet[t] {
			merged = append(merged, t)
		}
	}
	return b.setTags(pid, merged)
}

func (b *SQLiteBackend) RemoveTags(slug string, tags []string) error {
	pid, err := b.pageID(slug)
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("page %q not found", slug)
	}

	removeSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		removeSet[strings.TrimSpace(strings.ToLower(t))] = true
	}

	existing, err := b.GetTags(slug)
	if err != nil {
		return err
	}
	var kept []string
	for _, t := range existing {
		if !removeSet[t] {
			kept = append(kept, t)
		}
	}
	return b.setTags(pid, kept)
}

func (b *SQLiteBackend) GetTags(slug string) ([]string, error) {
	rows, err := b.db.Query(`
		SELECT t.tag FROM memory_tags t
		JOIN memory_pages p ON t.page_id = p.id
		WHERE p.slug = ?
		ORDER BY t.tag`, slug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// --- Links ---

func (b *SQLiteBackend) CreateLink(fromSlug, toSlug, linkType, context string) error {
	fromID, err := b.pageID(fromSlug)
	if err != nil {
		return err
	}
	if fromID == 0 {
		return fmt.Errorf("page %q not found", fromSlug)
	}

	toID, err := b.pageID(toSlug)
	if err != nil {
		return err
	}
	if toID == 0 {
		return fmt.Errorf("page %q not found", toSlug)
	}

	if linkType == "" {
		linkType = "mentions"
	}

	_, err = b.db.Exec(`
		INSERT OR IGNORE INTO memory_links (from_page_id, to_page_id, link_type, context)
		VALUES (?, ?, ?, ?)`,
		fromID, toID, linkType, context,
	)
	return err
}

func (b *SQLiteBackend) DeleteLink(fromSlug, toSlug, linkType string) error {
	fromID, err := b.pageID(fromSlug)
	if err != nil {
		return err
	}
	toID, err := b.pageID(toSlug)
	if err != nil {
		return err
	}
	if linkType == "" {
		// Delete all link types between the pair
		_, err = b.db.Exec(`DELETE FROM memory_links WHERE from_page_id = ? AND to_page_id = ?`, fromID, toID)
	} else {
		_, err = b.db.Exec(`DELETE FROM memory_links WHERE from_page_id = ? AND to_page_id = ? AND link_type = ?`, fromID, toID, linkType)
	}
	return err
}

func (b *SQLiteBackend) GetLinks(slug string) ([]LinkInfo, error) {
	pid, err := b.pageID(slug)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, fmt.Errorf("page %q not found", slug)
	}

	var links []LinkInfo

	// Outgoing
	rows, err := b.db.Query(`
		SELECT p.slug, l.link_type, l.context
		FROM memory_links l
		JOIN memory_pages p ON l.to_page_id = p.id
		WHERE l.from_page_id = ?
		ORDER BY l.link_type, p.slug`, pid,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var li LinkInfo
		if err := rows.Scan(&li.Slug, &li.LinkType, &li.Context); err != nil {
			rows.Close()
			return nil, err
		}
		li.Direction = "outgoing"
		links = append(links, li)
	}
	rows.Close()

	// Incoming
	rows, err = b.db.Query(`
		SELECT p.slug, l.link_type, l.context
		FROM memory_links l
		JOIN memory_pages p ON l.from_page_id = p.id
		WHERE l.to_page_id = ?
		ORDER BY l.link_type, p.slug`, pid,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var li LinkInfo
		if err := rows.Scan(&li.Slug, &li.LinkType, &li.Context); err != nil {
			rows.Close()
			return nil, err
		}
		li.Direction = "incoming"
		links = append(links, li)
	}
	rows.Close()

	return links, nil
}

// --- Timeline ---

func (b *SQLiteBackend) AddTimelineEntry(slug string, entry TimelineEntry) error {
	pid, err := b.pageID(slug)
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("page %q not found", slug)
	}

	_, err = b.db.Exec(`
		INSERT OR IGNORE INTO memory_timeline (page_id, event_date, summary)
		VALUES (?, ?, ?)`,
		pid, entry.EventDate, entry.Summary,
	)
	return err
}

func (b *SQLiteBackend) GetTimeline(slug string) ([]TimelineEntry, error) {
	rows, err := b.db.Query(`
		SELECT t.event_date, t.summary
		FROM memory_timeline t
		JOIN memory_pages p ON t.page_id = p.id
		WHERE p.slug = ?
		ORDER BY t.event_date DESC`, slug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(&e.EventDate, &e.Summary); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- Revisions & stats ---

func (b *SQLiteBackend) insertRevision(pageID int, typ, title, content, frontmatter, hash string) error {
	_, err := b.db.Exec(`
		INSERT INTO memory_page_revisions (page_id, type, title, content, frontmatter, content_hash)
		VALUES (?, ?, ?, ?, ?, ?)`,
		pageID, typ, title, content, frontmatter, hash,
	)
	return err
}

func (b *SQLiteBackend) ListRevisions(slug string) ([]PageRevision, error) {
	rows, err := b.db.Query(`
		SELECT r.id, r.created_at, r.type, r.title, COALESCE(r.content_hash, '')
		FROM memory_page_revisions r
		JOIN memory_pages p ON p.id = r.page_id
		WHERE p.slug = ?
		ORDER BY r.created_at DESC
		LIMIT 200`, slug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PageRevision
	for rows.Next() {
		var r PageRevision
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Type, &r.Title, &r.ContentHash); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *SQLiteBackend) RevertToRevision(slug string, revisionID int) error {
	var pageID int
	var curType, curTitle, curContent, curFM, curHash string
	err := b.db.QueryRow(`
		SELECT p.id, p.type, p.title, p.content, p.frontmatter, COALESCE(p.content_hash, '')
		FROM memory_pages p WHERE p.slug = ?`, slug,
	).Scan(&pageID, &curType, &curTitle, &curContent, &curFM, &curHash)
	if err == sql.ErrNoRows {
		return fmt.Errorf("page %q not found", slug)
	}
	if err != nil {
		return err
	}

	var revType, revTitle, revContent, revFM, revHash string
	err = b.db.QueryRow(`
		SELECT r.type, r.title, r.content, r.frontmatter, COALESCE(r.content_hash, '')
		FROM memory_page_revisions r
		JOIN memory_pages p ON p.id = r.page_id
		WHERE r.id = ? AND p.slug = ?`, revisionID, slug,
	).Scan(&revType, &revTitle, &revContent, &revFM, &revHash)
	if err == sql.ErrNoRows {
		return fmt.Errorf("revision %d not found for %q", revisionID, slug)
	}
	if err != nil {
		return err
	}

	if curType == revType && curTitle == revTitle && curContent == revContent && curFM == revFM {
		return nil
	}
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO memory_page_revisions (page_id, type, title, content, frontmatter, content_hash)
		VALUES (?, ?, ?, ?, ?, ?)`,
		pageID, curType, curTitle, curContent, curFM, curHash,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		UPDATE memory_pages SET type=?, title=?, content=?, frontmatter=?, content_hash=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`,
		revType, revTitle, revContent, revFM, contentHash(revContent), pageID,
	)
	if err != nil {
		return err
	}
	if b.hasFTS5 {
		_, _ = tx.Exec(`DELETE FROM memory_pages_fts WHERE rowid = ?`, pageID)
		_, _ = tx.Exec(`INSERT INTO memory_pages_fts(rowid, title, content) VALUES (?, ?, ?)`, pageID, revTitle, revContent)
	}
	return tx.Commit()
}

func (b *SQLiteBackend) Stats() (MemoryStats, error) {
	var nPage, nRev int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM memory_pages`).Scan(&nPage)
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM memory_page_revisions`).Scan(&nRev)
	return MemoryStats{
		Backend:       "sqlite",
		Fulltext:      b.hasFTS5,
		PageCount:     nPage,
		RevisionCount: nRev,
		DataSource:    b.dataSourceName,
	}, nil
}

// --- Pinned ---

func (b *SQLiteBackend) GetPinnedPages() ([]Page, error) {
	rows, err := b.db.Query(`
		SELECT id, slug, type, title, content, frontmatter, content_hash, created_at, updated_at
		FROM memory_pages
		WHERE slug LIKE 'config/%'
		ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.Slug, &p.Type, &p.Title, &p.Content, &p.Frontmatter, &p.ContentHash, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

func (b *SQLiteBackend) Close() error {
	return b.db.Close()
}

// HasFTS5 reports whether FTS5 is active.
func (b *SQLiteBackend) HasFTS5() bool {
	return b.hasFTS5
}
