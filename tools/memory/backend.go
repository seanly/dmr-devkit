package memory

import (
	"context"
	"io"
)

// Backend is the storage interface for the memory plugin.
type Backend interface {
	// Pages
	PutPage(slug string, input PageInput) (Page, error)
	GetPage(slug string) (*Page, error)
	DeletePage(slug string) error
	ListPages(opts ListOpts) ([]Page, error)
	SearchPages(query string, limit int) ([]SearchResult, error)

	// Tags
	AddTags(slug string, tags []string) error
	RemoveTags(slug string, tags []string) error
	GetTags(slug string) ([]string, error)

	// Links
	CreateLink(fromSlug, toSlug, linkType, context string) error
	DeleteLink(fromSlug, toSlug, linkType string) error
	GetLinks(slug string) ([]LinkInfo, error)

	// Timeline
	AddTimelineEntry(slug string, entry TimelineEntry) error
	GetTimeline(slug string) ([]TimelineEntry, error)

	// Pinned pages for prompt injection (future use)
	GetPinnedPages() ([]Page, error)

	// ListRevisions returns past snapshots for a page, newest first.
	ListRevisions(slug string) ([]PageRevision, error)

	// RevertToRevision restores a page to a saved revision. Current content is
	// snapshotted as a new revision first (if the page exists).
	RevertToRevision(slug string, revisionID int) error

	// Stats returns live counts and backend hint for observability.
	Stats() (MemoryStats, error)

	// HasFTS5 reports whether full-text search is active (SQLite: FTS5; Postgres: tsvector+GIN).
	HasFTS5() bool

	Close() error

	// Attachments (blobs via AttachmentBlobStore on backend impl)

	PutAttachment(ctx context.Context, slug string, input AttachmentInput, body io.Reader, size int64) (*Attachment, error)
	GetAttachment(publicID string) (*Attachment, error)
	OpenAttachment(ctx context.Context, publicID string) (io.ReadCloser, *Attachment, error)
	ListAttachments(slug string, limit int) ([]Attachment, error)
	DeleteAttachment(ctx context.Context, publicID string) error
	SearchAttachments(query string, limit int) ([]AttachmentSearchResult, error)
}
