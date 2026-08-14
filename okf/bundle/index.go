package bundle

import (
	"strings"

	"github.com/seanly/dmr-devkit/okf/markdown"
)

// IndexEntry represents a line in the bundle index.md table of contents.
type IndexEntry struct {
	ID          string // concept ID
	Title       string // display title
	Link        string // markdown link target
	Level       int    // heading level (2 = ##, 3 = ###, etc.)
	Description string // optional description
}

// ParseIndex parses the bundle's root index.md into structured entries.
// It looks for headings and markdown links pointing to concepts.
func (b *Bundle) ParseIndex() []IndexEntry {
	return b.parseIndexContent(b.Index)
}

// ParseIndexAt parses the index.md of a directory (dir-relative path, "." = root).
// Returns nil if no index.md exists for that directory.
func (b *Bundle) ParseIndexAt(dir string) []IndexEntry {
	return b.parseIndexContent(b.Indexes[dir])
}

// parseIndexContent is the shared link/heading extractor used by ParseIndex and
// ParseIndexAt. Directory links (no .md) are normalized to "<dir>/index".
func (b *Bundle) parseIndexContent(content string) []IndexEntry {
	if content == "" {
		return nil
	}
	var entries []IndexEntry
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Detect heading level
		level := 0
		for i := 0; i < len(line) && i < 6 && line[i] == '#'; i++ {
			level++
		}

		// Extract links from the line
		links := markdown.ExtractLinks(line)
		for _, lk := range links {
			id := strings.TrimPrefix(lk.URL, "/")
			id = strings.TrimSuffix(id, "/")
			id = strings.TrimSuffix(id, ".md")
			entries = append(entries, IndexEntry{
				ID:    id,
				Title: lk.Text,
				Link:  lk.URL,
				Level: level,
			})
		}
	}
	return entries
}

// MissingFromIndex returns concepts present in the bundle but not referenced in
// the root index.md.
func (b *Bundle) MissingFromIndex() []string {
	indexed := make(map[string]bool)
	for _, e := range b.ParseIndex() {
		indexed[e.ID] = true
	}

	var missing []string
	for id := range b.Concepts {
		if !indexed[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// MissingFromAnyIndex returns concepts not referenced in their own directory's
// index.md (or in the root index). A concept is considered covered if it appears
// in the index of its own directory or the root index.
func (b *Bundle) MissingFromAnyIndex() []string {
	// Root-indexed concept IDs.
	rootIndexed := make(map[string]bool)
	for _, e := range b.ParseIndex() {
		rootIndexed[e.ID] = true
	}

	var missing []string
	for id := range b.Concepts {
		if rootIndexed[id] {
			continue
		}
		dir := dirRel(id + ".md") // concept ID has no .md; reconstruct to get dir
		covered := false
		for _, e := range b.ParseIndexAt(dir) {
			if e.ID == id {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, id)
		}
	}
	return missing
}

// OrphanedLinks returns link targets in concepts that don't exist in the bundle.
func (b *Bundle) OrphanedLinks() map[string][]string {
	orphans := make(map[string][]string)
	for id, c := range b.Concepts {
		for _, target := range c.Links {
			if b.Get(target) == nil {
				orphans[id] = append(orphans[id], target)
			}
		}
	}
	return orphans
}
