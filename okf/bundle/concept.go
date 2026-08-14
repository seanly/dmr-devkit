package bundle

import (
	"github.com/seanly/dmr-devkit/okf/frontmatter"
)

// Concept represents a single OKF knowledge concept loaded from a .md file.
type Concept struct {
	// ID is the bundle-relative path without .md suffix (e.g. "tables/orders").
	ID string

	// Frontmatter fields.
	Meta *frontmatter.Meta

	// Body is the raw markdown content after frontmatter.
	Body string

	// Links are markdown links found in the body (relative paths only).
	Links []string

	// FilePath is the absolute path on disk.
	FilePath string
}

// TrustTier is a convenience accessor for the derived trust tier.
func (c *Concept) TrustTier() frontmatter.TrustTier {
	if c.Meta == nil {
		return frontmatter.Unverified
	}
	return c.Meta.DeriveTrustTier()
}

// IsStale reports whether the concept is past its stale_after date.
func (c *Concept) IsStale() bool {
	if c.Meta == nil {
		return false
	}
	return c.Meta.IsStale()
}

// IsDeprecated reports whether the concept is marked deprecated.
func (c *Concept) IsDeprecated() bool {
	if c.Meta == nil {
		return false
	}
	return c.Meta.IsDeprecated()
}

// Summary returns a short string suitable for search result listings.
func (c *Concept) Summary() string {
	if c.Meta == nil {
		return ""
	}
	if c.Meta.Description != "" {
		return c.Meta.Description
	}
	return c.Body
}
