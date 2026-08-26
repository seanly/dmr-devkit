package memory

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (b *SQLiteBackend) deleteAttachmentBlobsForPage(pageID int) error {
	if b.blob == nil {
		return nil
	}
	rows, err := b.db.Query(`SELECT public_id FROM memory_attachments WHERE page_id = ?`, pageID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pub string
		if err := rows.Scan(&pub); err != nil {
			return err
		}
		_ = b.blob.Delete(context.Background(), pub)
	}
	return rows.Err()
}

// PutAttachment implements Backend.
func (b *SQLiteBackend) PutAttachment(ctx context.Context, slug string, input AttachmentInput, body io.Reader, size int64) (*Attachment, error) {
	if b.blob == nil {
		return nil, fmt.Errorf("attachment blob store not configured")
	}
	pid, err := b.pageID(slug)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, fmt.Errorf("page %q not found", slug)
	}
	publicID := uuid.NewString()
	summary := clampAttachmentSummary(input.Summary)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("attachment name is required")
	}
	mime := strings.TrimSpace(input.Mime)
	if mime == "" {
		mime = "application/octet-stream"
	}
	if err := b.blob.Put(ctx, publicID, mime, body, size); err != nil {
		return nil, err
	}
	res, err := b.db.Exec(`
		INSERT INTO memory_attachments (public_id, page_id, name, mime, size, summary, storage_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		publicID, pid, name, mime, size, summary, b.attachmentKind,
	)
	if err != nil {
		_ = b.blob.Delete(ctx, publicID)
		return nil, err
	}
	aid, err := res.LastInsertId()
	if err != nil {
		_ = b.blob.Delete(ctx, publicID)
		return nil, err
	}
	if b.hasAttachmentFTS5 {
		_, _ = b.db.Exec(`INSERT INTO memory_attachments_fts(rowid, name, summary) VALUES (?, ?, ?)`, aid, name, summary)
	}
	return b.GetAttachment(publicID)
}

// GetAttachment implements Backend.
func (b *SQLiteBackend) GetAttachment(publicID string) (*Attachment, error) {
	publicID = strings.TrimSpace(publicID)
	if _, err := uuid.Parse(publicID); err != nil {
		return nil, fmt.Errorf("invalid attachment id: %w", err)
	}
	var a Attachment
	var created, updated string
	err := b.db.QueryRow(`
		SELECT a.public_id, p.slug, a.name, a.mime, a.size, a.summary, a.storage_kind, a.created_at, a.updated_at
		FROM memory_attachments a
		JOIN memory_pages p ON p.id = a.page_id
		WHERE a.public_id = ?`, publicID,
	).Scan(&a.PublicID, &a.Slug, &a.Name, &a.Mime, &a.Size, &a.Summary, &a.StorageKind, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	if t2, e := time.Parse(time.RFC3339, created); e == nil {
		a.CreatedAt = t2
	}
	a.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updated)
	if t2, e := time.Parse(time.RFC3339, updated); e == nil {
		a.UpdatedAt = t2
	}
	return &a, nil
}

// OpenAttachment implements Backend.
func (b *SQLiteBackend) OpenAttachment(ctx context.Context, publicID string) (io.ReadCloser, *Attachment, error) {
	if b.blob == nil {
		return nil, nil, fmt.Errorf("attachment blob store not configured")
	}
	meta, err := b.GetAttachment(publicID)
	if err != nil {
		return nil, nil, err
	}
	if meta == nil {
		return nil, nil, fmt.Errorf("attachment not found")
	}
	rc, err := b.blob.Open(ctx, strings.TrimSpace(publicID))
	if err != nil {
		return nil, nil, err
	}
	return rc, meta, nil
}

// ListAttachments implements Backend.
func (b *SQLiteBackend) ListAttachments(slug string, limit int) ([]Attachment, error) {
	if limit <= 0 {
		limit = 50
	}
	pid, err := b.pageID(slug)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, fmt.Errorf("page %q not found", slug)
	}
	rows, err := b.db.Query(`
		SELECT a.public_id, p.slug, a.name, a.mime, a.size, a.summary, a.storage_kind, a.created_at, a.updated_at
		FROM memory_attachments a
		JOIN memory_pages p ON p.id = a.page_id
		WHERE a.page_id = ?
		ORDER BY a.created_at DESC
		LIMIT ?`, pid, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func scanAttachmentRows(rows *sql.Rows) ([]Attachment, error) {
	var out []Attachment
	for rows.Next() {
		var a Attachment
		var created, updated string
		if err := rows.Scan(&a.PublicID, &a.Slug, &a.Name, &a.Mime, &a.Size, &a.Summary, &a.StorageKind, &created, &updated); err != nil {
			return nil, err
		}
		parseSQLiteTime(&a.CreatedAt, &a.UpdatedAt, created, updated)
		out = append(out, a)
	}
	return out, rows.Err()
}

func parseSQLiteTime(created, updated *time.Time, cs, us string) {
	if t, e := time.Parse(time.RFC3339, cs); e == nil {
		*created = t
	} else if t, e := time.Parse("2006-01-02 15:04:05", cs); e == nil {
		*created = t
	}
	if t, e := time.Parse(time.RFC3339, us); e == nil {
		*updated = t
	} else if t, e := time.Parse("2006-01-02 15:04:05", us); e == nil {
		*updated = t
	}
}

// DeleteAttachment implements Backend.
func (b *SQLiteBackend) DeleteAttachment(ctx context.Context, publicID string) error {
	if b.blob == nil {
		return fmt.Errorf("attachment blob store not configured")
	}
	publicID = strings.TrimSpace(publicID)
	var rowID int
	err := b.db.QueryRow(`SELECT id FROM memory_attachments WHERE public_id = ?`, publicID).Scan(&rowID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("attachment not found")
	}
	if err != nil {
		return err
	}
	if b.hasAttachmentFTS5 {
		_, _ = b.db.Exec(`DELETE FROM memory_attachments_fts WHERE rowid = ?`, rowID)
	}
	_, err = b.db.Exec(`DELETE FROM memory_attachments WHERE public_id = ?`, publicID)
	if err != nil {
		return err
	}
	return b.blob.Delete(ctx, publicID)
}

// SearchAttachments implements Backend.
func (b *SQLiteBackend) SearchAttachments(query string, limit int) ([]AttachmentSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	if b.hasAttachmentFTS5 {
		rows, err := b.db.Query(`
			SELECT a.public_id, p.slug, a.name,
				snippet(memory_attachments_fts, 0, '<<', '>>', '...', 24) as snippet, bm25(memory_attachments_fts)
			FROM memory_attachments_fts
			JOIN memory_attachments a ON a.id = memory_attachments_fts.rowid
			JOIN memory_pages p ON p.id = a.page_id
			WHERE memory_attachments_fts MATCH ?
			ORDER BY bm25(memory_attachments_fts)
			LIMIT ?`,
			query, limit,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []AttachmentSearchResult
		for rows.Next() {
			var r AttachmentSearchResult
			if err := rows.Scan(&r.PublicID, &r.Slug, &r.Name, &r.Snippet, &r.Rank); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	pattern := "%" + query + "%"
	rows, err := b.db.Query(`
		SELECT a.public_id, p.slug, a.name,
			substr(coalesce(a.summary,'') || ' ' || coalesce(a.name,''), 1, 200) as snippet, 0.0
		FROM memory_attachments a
		JOIN memory_pages p ON p.id = a.page_id
		WHERE a.name LIKE ? OR a.summary LIKE ?
		ORDER BY a.updated_at DESC
		LIMIT ?`,
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttachmentSearchResult
	for rows.Next() {
		var r AttachmentSearchResult
		if err := rows.Scan(&r.PublicID, &r.Slug, &r.Name, &r.Snippet, &r.Rank); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func clampAttachmentSummary(s string) string {
	s = strings.TrimSpace(s)
	const maxRunes = 4096
	r := []rune(s)
	if len(r) > maxRunes {
		return string(r[:maxRunes])
	}
	return s
}
