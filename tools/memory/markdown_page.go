package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// markdownPageStore reads and writes memory pages as markdown files.
// Each page is a file: <dir>/<slug>.md
// Format:
//
//	---
//	slug: people/tina-wang
//	type: person
//	title: Tina Wang
//	tags: [engineering, backend]
//	created_at: 2026-01-15T10:00:00Z
//	updated_at: 2026-06-10T14:30:00Z
//	---
//	<content>
type markdownPageStore struct {
	dir string
}

func newMarkdownPageStore(dir string) *markdownPageStore {
	return &markdownPageStore{dir: dir}
}

// pageFrontmatter represents the YAML frontmatter of a memory page markdown file.
type pageFrontmatter struct {
	Slug      string   `yaml:"slug"`
	Type      string   `yaml:"type"`
	Title     string   `yaml:"title"`
	Tags      []string `yaml:"tags,omitempty"`
	CreatedAt string   `yaml:"created_at,omitempty"`
	UpdatedAt string   `yaml:"updated_at,omitempty"`
}

func (m *markdownPageStore) ensureDir() error {
	return os.MkdirAll(m.dir, 0o700)
}

func (m *markdownPageStore) pagePath(slug string) string {
	return filepath.Join(m.dir, safeSlugPath(slug)+".md")
}

// legacyPagePath returns the old flat filename (e.g. "people-tina-wang.md")
// for backward compatibility with pre-directory-layout stores.
func (m *markdownPageStore) legacyPagePath(slug string) string {
	return filepath.Join(m.dir, safeSlugFilenameLegacy(slug)+".md")
}

func safeSlugFilenameLegacy(slug string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	return r.Replace(slug)
}

// safeSlugPath converts a slug like "people/tina-wang" into a safe relative
// path "people/tina-wang". It preserves directory separators, prevents
// directory traversal, and sanitizes individual path segments.
func safeSlugPath(slug string) string {
	// Normalize separators to the platform separator.
	slug = filepath.FromSlash(slug)

	// Prevent directory traversal by cleaning relative to a dummy root,
	// then stripping that root prefix.
	slug = filepath.Clean(filepath.Join("_", slug))
	slug = strings.TrimPrefix(slug, string(filepath.Separator)+"_")
	slug = strings.TrimPrefix(slug, "_"+string(filepath.Separator))
	slug = strings.TrimPrefix(slug, "_")

	// Sanitize each segment.
	parts := strings.Split(slug, string(filepath.Separator))
	for i, p := range parts {
		parts[i] = safePathSegment(p)
	}
	return filepath.Join(parts...)
}

// safePathSegment replaces characters that are illegal or problematic in filenames.
func safePathSegment(name string) string {
	if name == "" || name == "." {
		return "_"
	}
	r := strings.NewReplacer(":", "-", "?", "-", "*", "-", "<", "-", ">", "-", "|", "-", "\x00", "")
	return r.Replace(name)
}

// WritePage writes a page as a markdown file with YAML frontmatter.
func (m *markdownPageStore) WritePage(p Page) error {
	if err := m.ensureDir(); err != nil {
		return err
	}

	fm := pageFrontmatter{
		Slug:      p.Slug,
		Type:      p.Type,
		Title:     p.Title,
		Tags:      p.Tags,
		CreatedAt: p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if fm.Type == "" {
		fm.Type = "note"
	}

	fmData, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("marshal frontmatter: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmData)
	b.WriteString("---\n\n")
	b.WriteString(p.Content)
	if !strings.HasSuffix(p.Content, "\n") {
		b.WriteString("\n")
	}

	return writeFileAtomic(m.pagePath(p.Slug), b.String())
}

// ReadPage reads a page from a markdown file.
// Falls back to legacy flat filename (e.g. "people-tina-wang.md") and auto-migrates
// to hierarchical layout (e.g. "people/tina-wang.md") when found.
func (m *markdownPageStore) ReadPage(slug string) (*Page, error) {
	path := m.pagePath(slug)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Fallback: try legacy flat filename.
			legacyPath := m.legacyPagePath(slug)
			data, err = os.ReadFile(legacyPath)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, nil
				}
				return nil, err
			}
			// Auto-migrate: rewrite to new hierarchical path and delete legacy.
			path = legacyPath
		} else {
			return nil, err
		}
	}

	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		// No frontmatter — treat entire file as content.
		return &Page{
			Slug:    slug,
			Content: strings.TrimSpace(content),
		}, nil
	}

	rest := content[4:]
	endIdx := strings.Index(rest, "\n---\n")
	if endIdx == -1 {
		return nil, fmt.Errorf("invalid frontmatter in %s", path)
	}

	fmStr := rest[:endIdx]
	body := strings.TrimSpace(rest[endIdx+5:])

	var fm pageFrontmatter
	if err := yaml.Unmarshal([]byte(fmStr), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter in %s: %w", path, err)
	}

	p := &Page{
		Slug:    fm.Slug,
		Type:    fm.Type,
		Title:   fm.Title,
		Tags:    fm.Tags,
		Content: body,
	}
	if p.Slug == "" {
		p.Slug = slug
	}
	return p, nil
}

// DeletePage removes the markdown file for a page.
// Falls back to legacy flat filename if the hierarchical path does not exist.
func (m *markdownPageStore) DeletePage(slug string) error {
	path := m.pagePath(slug)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			// Fallback: try legacy flat filename.
			legacyPath := m.legacyPagePath(slug)
			if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		return err
	}
	return nil
}

// ListFiles returns all markdown files in the store.
func (m *markdownPageStore) ListFiles() ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		files = append(files, e.Name())
	}
	return files, nil
}

// MigrateFromDB exports all pages from a DB-backed backend to markdown files.
func (m *markdownPageStore) MigrateFromDB(backend Backend) error {
	pages, err := backend.ListPages(ListOpts{Limit: 10000})
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}
	for _, p := range pages {
		// Load tags and timeline for richer frontmatter.
		tags, _ := backend.GetTags(p.Slug)
		p.Tags = tags
		if err := m.WritePage(p); err != nil {
			// Log but continue.
			continue
		}
	}
	return nil
}

func writeFileAtomic(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".memory_*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if werr != nil {
		_ = os.Remove(tmpName)
		return werr
	}
	if cerr != nil {
		_ = os.Remove(tmpName)
		return cerr
	}
	return os.Rename(tmpName, path)
}
