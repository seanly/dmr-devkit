package memory

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresBackend implements Backend with PostgreSQL + tsvector full-text search.
type PostgresBackend struct {
	db               *sql.DB
	hasFTS           bool
	hasAttachmentFTS bool
	blob             AttachmentBlobStore
	attachmentKind   string // "local" | "s3" for stored rows
}

// NewPostgresBackend creates a new PostgreSQL-backed store.
func NewPostgresBackend(cfg Config, blob AttachmentBlobStore) (*PostgresBackend, error) {
	db, err := sql.Open("pgx", cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	kind := strings.ToLower(strings.TrimSpace(cfg.AttachmentBackend))
	switch kind {
	case "s3", "minio":
		kind = "s3"
	case "local", "fs", "file", "":
		kind = "local"
	default:
		kind = "local"
	}

	b := &PostgresBackend{
		db:             db,
		blob:           blob,
		attachmentKind: kind,
	}

	if err := b.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	if cfg.EnableFTS5 {
		if err := b.initFTS(); err != nil {
			log.Printf("[memory] postgres: full-text (tsvector) init failed, using LIKE search: %v", err)
		} else {
			b.hasFTS = true
			b.hasAttachmentFTS = true
		}
	}

	return b, nil
}

func (b *PostgresBackend) initSchema() error {
	_, err := b.db.Exec(`
		CREATE TABLE IF NOT EXISTS memory_pages (
			id SERIAL PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL DEFAULT 'note',
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			frontmatter TEXT NOT NULL DEFAULT '{}',
			content_hash TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memory_pages_type ON memory_pages(type);

		CREATE TABLE IF NOT EXISTS memory_tags (
			id SERIAL PRIMARY KEY,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			tag TEXT NOT NULL,
			UNIQUE(page_id, tag)
		);
		CREATE INDEX IF NOT EXISTS idx_memory_tags_tag ON memory_tags(tag);
		CREATE INDEX IF NOT EXISTS idx_memory_tags_page ON memory_tags(page_id);

		CREATE TABLE IF NOT EXISTS memory_links (
			id SERIAL PRIMARY KEY,
			from_page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			to_page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			link_type TEXT NOT NULL DEFAULT 'mentions',
			context TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(from_page_id, to_page_id, link_type)
		);
		CREATE INDEX IF NOT EXISTS idx_memory_links_from ON memory_links(from_page_id);
		CREATE INDEX IF NOT EXISTS idx_memory_links_to ON memory_links(to_page_id);

		CREATE TABLE IF NOT EXISTS memory_timeline (
			id SERIAL PRIMARY KEY,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			event_date TEXT NOT NULL,
			summary TEXT NOT NULL,
			UNIQUE(page_id, event_date, summary)
		);
		CREATE INDEX IF NOT EXISTS idx_memory_timeline_page ON memory_timeline(page_id);
		CREATE INDEX IF NOT EXISTS idx_memory_timeline_date ON memory_timeline(event_date);

		CREATE TABLE IF NOT EXISTS memory_page_revisions (
			id SERIAL PRIMARY KEY,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			type TEXT NOT NULL DEFAULT 'note',
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			frontmatter TEXT NOT NULL DEFAULT '{}',
			content_hash TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memory_revisions_page ON memory_page_revisions(page_id, created_at);

		CREATE TABLE IF NOT EXISTS memory_attachments (
			id SERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			page_id INTEGER NOT NULL REFERENCES memory_pages(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			mime TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			summary TEXT NOT NULL DEFAULT '',
			storage_kind TEXT NOT NULL DEFAULT 'local',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memory_attachments_page ON memory_attachments(page_id);
	`)
	return err
}

func (b *PostgresBackend) initFTS() error {
	_, err := b.db.Exec(`
		ALTER TABLE memory_pages 
		ADD COLUMN IF NOT EXISTS search_vector tsvector 
		GENERATED ALWAYS AS (to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(content, ''))) STORED;

		CREATE INDEX IF NOT EXISTS idx_memory_pages_fts ON memory_pages USING GIN(search_vector);

		ALTER TABLE memory_attachments
		ADD COLUMN IF NOT EXISTS search_vector tsvector
		GENERATED ALWAYS AS (to_tsvector('simple', coalesce(name, '') || ' ' || coalesce(summary, ''))) STORED;

		CREATE INDEX IF NOT EXISTS idx_memory_attachments_fts ON memory_attachments USING GIN(search_vector);
	`)
	return err
}

func (b *PostgresBackend) pageID(slug string) (int, error) {
	var id int
	err := b.db.QueryRow(`SELECT id FROM memory_pages WHERE slug = $1`, slug).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (b *PostgresBackend) PutPage(slug string, input PageInput) (Page, error) {
	h := contentHash(input.Content)
	fm := input.Frontmatter
	if fm == "" {
		fm = "{}"
	}
	typ := input.Type
	if typ == "" {
		typ = "note"
	}

	var priorID int
	var pType, pTitle, pContent, pFM, pHash string
	err := b.db.QueryRow(`
		SELECT id, type, title, content, frontmatter, COALESCE(content_hash, '')
		FROM memory_pages WHERE slug = $1`, slug,
	).Scan(&priorID, &pType, &pTitle, &pContent, &pFM, &pHash)
	if err == nil {
		if pType != typ || pTitle != input.Title || pContent != input.Content || pFM != fm {
			_, e := b.db.Exec(`
				INSERT INTO memory_page_revisions (page_id, type, title, content, frontmatter, content_hash)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				priorID, pType, pTitle, pContent, pFM, pHash,
			)
			if e != nil {
				return Page{}, e
			}
		}
	} else if err != sql.ErrNoRows {
		return Page{}, err
	}

	var pageID int
	err = b.db.QueryRow(`
		INSERT INTO memory_pages (slug, type, title, content, frontmatter, content_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (slug) DO UPDATE SET
			type = EXCLUDED.type,
			title = EXCLUDED.title,
			content = EXCLUDED.content,
			frontmatter = EXCLUDED.frontmatter,
			content_hash = EXCLUDED.content_hash,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id`,
		slug, typ, input.Title, input.Content, fm, h,
	).Scan(&pageID)
	if err != nil {
		return Page{}, err
	}

	if len(input.Tags) > 0 {
		if err := b.setTags(pageID, input.Tags); err != nil {
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
	return *p, nil
}

func (b *PostgresBackend) GetPage(slug string) (*Page, error) {
	p := &Page{}
	err := b.db.QueryRow(`
		SELECT id, slug, type, title, content, frontmatter, content_hash, created_at, updated_at
		FROM memory_pages WHERE slug = $1`, slug,
	).Scan(&p.ID, &p.Slug, &p.Type, &p.Title, &p.Content, &p.Frontmatter, &p.ContentHash, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	p.Tags, _ = b.GetTags(slug)
	p.Timeline, _ = b.GetTimeline(slug)
	return p, nil
}

func (b *PostgresBackend) DeletePage(slug string) error {
	pid, err := b.pageID(slug)
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("page %q not found", slug)
	}
	if err := b.deleteAttachmentBlobsForPage(pid); err != nil {
		return err
	}
	res, err := b.db.Exec(`DELETE FROM memory_pages WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("page %q not found", slug)
	}
	return nil
}

func (b *PostgresBackend) ListPages(opts ListOpts) ([]Page, error) {
	q := "SELECT id, slug, type, title, content, frontmatter, content_hash, created_at, updated_at FROM memory_pages WHERE 1=1"
	var args []any
	argIdx := 1

	if opts.Tag != "" {
		q += fmt.Sprintf(" AND id IN (SELECT page_id FROM memory_tags WHERE tag = $%d)", argIdx)
		args = append(args, opts.Tag)
		argIdx++
	}
	if opts.SlugPrefix != "" {
		q += fmt.Sprintf(" AND slug LIKE $%d", argIdx)
		args = append(args, opts.SlugPrefix+"%")
		argIdx++
	}
	if opts.Type != "" {
		q += fmt.Sprintf(" AND type = $%d", argIdx)
		args = append(args, opts.Type)
		argIdx++
	}

	q += " ORDER BY updated_at DESC"

	needsFmFilter := len(opts.FrontmatterFilter) > 0

	if !needsFmFilter {
		limit := opts.Limit
		if limit <= 0 {
			limit = 20
		}
		q += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, limit)
		argIdx++

		if opts.Offset > 0 {
			q += fmt.Sprintf(" OFFSET $%d", argIdx)
			args = append(args, opts.Offset)
			argIdx++
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

func (b *PostgresBackend) SearchPages(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}

	if b.hasFTS {
		results, err := b.searchTSVector(query, limit)
		if err == nil {
			return results, nil
		}
	}
	return b.searchLike(query, limit)
}

func (b *PostgresBackend) searchTSVector(query string, limit int) ([]SearchResult, error) {
	rows, err := b.db.Query(`
		SELECT p.slug, p.title, p.type,
			ts_headline('simple', p.content, plainto_tsquery('simple', $1)) as snippet,
			ts_rank(p.search_vector, plainto_tsquery('simple', $1)) as rank
		FROM memory_pages p
		WHERE p.search_vector @@ plainto_tsquery('simple', $1)
		ORDER BY rank DESC
		LIMIT $2`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchResults(rows)
}

func (b *PostgresBackend) searchLike(query string, limit int) ([]SearchResult, error) {
	pattern := "%" + query + "%"
	rows, err := b.db.Query(`
		SELECT slug, title, type, LEFT(content, 200) as snippet, 0.0
		FROM memory_pages
		WHERE title LIKE $1 OR content LIKE $1
		ORDER BY updated_at DESC
		LIMIT $2`,
		pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchResults(rows)
}

func (b *PostgresBackend) setTags(pageID int, tags []string) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM memory_tags WHERE page_id = $1`, pageID)
	if err != nil {
		return err
	}
	for _, t := range tags {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		_, err = tx.Exec(`
			INSERT INTO memory_tags (page_id, tag) VALUES ($1, $2)
			ON CONFLICT (page_id, tag) DO NOTHING`,
			pageID, t,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *PostgresBackend) AddTags(slug string, tags []string) error {
	pid, err := b.pageID(slug)
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("page %q not found", slug)
	}

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

func (b *PostgresBackend) RemoveTags(slug string, tags []string) error {
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

func (b *PostgresBackend) GetTags(slug string) ([]string, error) {
	rows, err := b.db.Query(`
		SELECT t.tag FROM memory_tags t
		JOIN memory_pages p ON t.page_id = p.id
		WHERE p.slug = $1
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

func (b *PostgresBackend) CreateLink(fromSlug, toSlug, linkType, context string) error {
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
		INSERT INTO memory_links (from_page_id, to_page_id, link_type, context)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (from_page_id, to_page_id, link_type) DO NOTHING`,
		fromID, toID, linkType, context,
	)
	return err
}

func (b *PostgresBackend) DeleteLink(fromSlug, toSlug, linkType string) error {
	fromID, err := b.pageID(fromSlug)
	if err != nil {
		return err
	}
	toID, err := b.pageID(toSlug)
	if err != nil {
		return err
	}
	if linkType == "" {
		_, err = b.db.Exec(`DELETE FROM memory_links WHERE from_page_id = $1 AND to_page_id = $2`, fromID, toID)
	} else {
		_, err = b.db.Exec(`DELETE FROM memory_links WHERE from_page_id = $1 AND to_page_id = $2 AND link_type = $3`, fromID, toID, linkType)
	}
	return err
}

func (b *PostgresBackend) GetLinks(slug string) ([]LinkInfo, error) {
	pid, err := b.pageID(slug)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, fmt.Errorf("page %q not found", slug)
	}

	var links []LinkInfo

	rows, err := b.db.Query(`
		SELECT p.slug, l.link_type, l.context
		FROM memory_links l
		JOIN memory_pages p ON l.to_page_id = p.id
		WHERE l.from_page_id = $1
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

	rows, err = b.db.Query(`
		SELECT p.slug, l.link_type, l.context
		FROM memory_links l
		JOIN memory_pages p ON l.from_page_id = p.id
		WHERE l.to_page_id = $1
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

func (b *PostgresBackend) AddTimelineEntry(slug string, entry TimelineEntry) error {
	pid, err := b.pageID(slug)
	if err != nil {
		return err
	}
	if pid == 0 {
		return fmt.Errorf("page %q not found", slug)
	}

	_, err = b.db.Exec(`
		INSERT INTO memory_timeline (page_id, event_date, summary)
		VALUES ($1, $2, $3)
		ON CONFLICT (page_id, event_date, summary) DO NOTHING`,
		pid, entry.EventDate, entry.Summary,
	)
	return err
}

func (b *PostgresBackend) GetTimeline(slug string) ([]TimelineEntry, error) {
	rows, err := b.db.Query(`
		SELECT t.event_date, t.summary
		FROM memory_timeline t
		JOIN memory_pages p ON t.page_id = p.id
		WHERE p.slug = $1
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

func (b *PostgresBackend) ListRevisions(slug string) ([]PageRevision, error) {
	rows, err := b.db.Query(`
		SELECT r.id, r.created_at, r.type, r.title, COALESCE(r.content_hash, '')
		FROM memory_page_revisions r
		JOIN memory_pages p ON p.id = r.page_id
		WHERE p.slug = $1
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

func (b *PostgresBackend) RevertToRevision(slug string, revisionID int) error {
	var pageID int
	var curType, curTitle, curContent, curFM, curHash string
	err := b.db.QueryRow(`
		SELECT p.id, p.type, p.title, p.content, p.frontmatter, COALESCE(p.content_hash, '')
		FROM memory_pages p WHERE p.slug = $1`, slug,
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
		WHERE r.id = $1 AND p.slug = $2`, revisionID, slug,
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
		VALUES ($1, $2, $3, $4, $5, $6)`,
		pageID, curType, curTitle, curContent, curFM, curHash,
	)
	if err != nil {
		return err
	}
	newHash := contentHash(revContent)
	_, err = tx.Exec(`
		UPDATE memory_pages SET type=$1, title=$2, content=$3, frontmatter=$4, content_hash=$5, updated_at=CURRENT_TIMESTAMP
		WHERE id=$6`,
		revType, revTitle, revContent, revFM, newHash, pageID,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (b *PostgresBackend) Stats() (MemoryStats, error) {
	var nPage, nRev int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM memory_pages`).Scan(&nPage)
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM memory_page_revisions`).Scan(&nRev)
	return MemoryStats{
		Backend:       "postgres",
		Fulltext:      b.hasFTS,
		PageCount:     nPage,
		RevisionCount: nRev,
		DataSource:    "postgres",
	}, nil
}

func (b *PostgresBackend) GetPinnedPages() ([]Page, error) {
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

func (b *PostgresBackend) Close() error {
	return b.db.Close()
}

// HasFTS5 reports whether full-text search is active (pages + attachments use the same toggle).
func (b *PostgresBackend) HasFTS5() bool {
	return b.hasFTS
}
