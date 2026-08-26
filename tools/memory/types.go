package memory

import "time"

// Page represents a knowledge page in the memory store.
type Page struct {
	ID          int             `json:"id"`
	Slug        string          `json:"slug"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Content     string          `json:"content"`
	Frontmatter string          `json:"frontmatter"`
	ContentHash string          `json:"content_hash,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Tags        []string        `json:"tags,omitempty"`
	Timeline    []TimelineEntry `json:"timeline,omitempty"`
}

// PageInput is the write payload for creating/updating a page.
type PageInput struct {
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	Frontmatter string   `json:"frontmatter"`
	Tags        []string `json:"tags"`
}

// LinkInfo describes a link to or from a page.
type LinkInfo struct {
	Slug      string `json:"slug"`
	LinkType  string `json:"link_type"`
	Context   string `json:"context"`
	Direction string `json:"direction"` // "outgoing" or "incoming"
}

// TimelineEntry represents a dated event on a page's timeline.
type TimelineEntry struct {
	EventDate string `json:"event_date"`
	Summary   string `json:"summary"`
}

// SearchResult represents a search hit.
type SearchResult struct {
	Slug    string  `json:"slug"`
	Title   string  `json:"title"`
	Type    string  `json:"type"`
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
}

// ListOpts filters for listing pages.
type ListOpts struct {
	Tag               string
	SlugPrefix        string
	Type              string
	Limit             int
	Offset            int
	FrontmatterFilter map[string]string // key=value exact match against parsed frontmatter JSON
}

// MemoryStats is returned by Backend.Stats (counts and backend capabilities).
type MemoryStats struct {
	Backend       string // "sqlite" or "postgres"
	Fulltext      bool
	PageCount     int
	RevisionCount int
	// DataSource is a non-secret hint: e.g. sqlite file base name, or "postgres".
	DataSource string
}

// PageRevision is a point-in-time snapshot of page fields before a write.
type PageRevision struct {
	ID          int       `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	ContentHash string    `json:"content_hash"`
}

// Attachment is a binary file linked to a memory page (metadata in DB; bytes in blob store).
type Attachment struct {
	PublicID    string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Mime        string    `json:"mime,omitempty"`
	Size        int64     `json:"size"`
	Summary     string    `json:"summary,omitempty"`
	StorageKind string    `json:"storage_kind,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AttachmentInput is the write payload for PutAttachment metadata.
type AttachmentInput struct {
	Name    string
	Mime    string
	Summary string
}

// AttachmentSearchResult is a full-text hit on attachment name/summary.
type AttachmentSearchResult struct {
	PublicID string  `json:"id"`
	Slug     string  `json:"slug"`
	Name     string  `json:"name"`
	Snippet  string  `json:"snippet"`
	Rank     float64 `json:"rank"`
}
