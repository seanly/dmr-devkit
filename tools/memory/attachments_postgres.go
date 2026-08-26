package memory

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

func (b *PostgresBackend) deleteAttachmentBlobsForPage(pageID int) error {
	if b.blob == nil {
		return nil
	}
	rows, err := b.db.Query(`SELECT public_id FROM memory_attachments WHERE page_id = $1`, pageID)
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
func (b *PostgresBackend) PutAttachment(ctx context.Context, slug string, input AttachmentInput, body io.Reader, size int64) (*Attachment, error) {
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
	_, err = b.db.Exec(`
		INSERT INTO memory_attachments (public_id, page_id, name, mime, size, summary, storage_kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		publicID, pid, name, mime, size, summary, b.attachmentKind,
	)
	if err != nil {
		_ = b.blob.Delete(ctx, publicID)
		return nil, err
	}
	return b.GetAttachment(publicID)
}

// GetAttachment implements Backend.
func (b *PostgresBackend) GetAttachment(publicID string) (*Attachment, error) {
	publicID = strings.TrimSpace(publicID)
	if _, err := uuid.Parse(publicID); err != nil {
		return nil, fmt.Errorf("invalid attachment id: %w", err)
	}
	var a Attachment
	err := b.db.QueryRow(`
		SELECT a.public_id, p.slug, a.name, a.mime, a.size, a.summary, a.storage_kind, a.created_at, a.updated_at
		FROM memory_attachments a
		JOIN memory_pages p ON p.id = a.page_id
		WHERE a.public_id = $1`, publicID,
	).Scan(&a.PublicID, &a.Slug, &a.Name, &a.Mime, &a.Size, &a.Summary, &a.StorageKind, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// OpenAttachment implements Backend.
func (b *PostgresBackend) OpenAttachment(ctx context.Context, publicID string) (io.ReadCloser, *Attachment, error) {
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
func (b *PostgresBackend) ListAttachments(slug string, limit int) ([]Attachment, error) {
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
		WHERE a.page_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2`, pid, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.PublicID, &a.Slug, &a.Name, &a.Mime, &a.Size, &a.Summary, &a.StorageKind, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAttachment implements Backend.
func (b *PostgresBackend) DeleteAttachment(ctx context.Context, publicID string) error {
	if b.blob == nil {
		return fmt.Errorf("attachment blob store not configured")
	}
	publicID = strings.TrimSpace(publicID)
	res, err := b.db.Exec(`DELETE FROM memory_attachments WHERE public_id = $1`, publicID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("attachment not found")
	}
	return b.blob.Delete(ctx, publicID)
}

// SearchAttachments implements Backend.
func (b *PostgresBackend) SearchAttachments(query string, limit int) ([]AttachmentSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	if b.hasAttachmentFTS {
		rows, err := b.db.Query(`
			SELECT a.public_id, p.slug, a.name,
				ts_headline('simple', coalesce(a.name,'') || ' ' || coalesce(a.summary,''), plainto_tsquery('simple', $1)) as snippet,
				ts_rank(a.search_vector, plainto_tsquery('simple', $1)) as rank
			FROM memory_attachments a
			JOIN memory_pages p ON p.id = a.page_id
			WHERE a.search_vector @@ plainto_tsquery('simple', $1)
			ORDER BY rank DESC
			LIMIT $2`,
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
			substring(coalesce(a.summary,'') || ' ' || coalesce(a.name,'') from 1 for 200) as snippet, 0.0
		FROM memory_attachments a
		JOIN memory_pages p ON p.id = a.page_id
		WHERE a.name ILIKE $1 OR a.summary ILIKE $1
		ORDER BY a.updated_at DESC
		LIMIT $2`,
		pattern, limit,
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
