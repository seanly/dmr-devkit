package bundle

import (
	"path/filepath"
	"strings"

	"github.com/seanly/dmr-devkit/okf/config"
	"github.com/seanly/dmr-devkit/okf/frontmatter"
)

// ParseError records a non-reserved .md file whose frontmatter could not be parsed.
// §11.1 要求每个非保留 .md 文件包含可解析的 YAML frontmatter；不可解析即违规。
type ParseError struct {
	FilePath string // absolute path
	RelPath  string // bundle-relative path
	Err      error
}

// Bundle is an in-memory representation of an OKF knowledge bundle.
type Bundle struct {
	RootPath string // absolute path to bundle root

	// Concepts keyed by Concept ID (bundle-relative path without .md).
	Concepts map[string]*Concept

	// Index is the parsed root index.md content (optional). 指向 Indexes["."]。
	// 保留为兼容字段；新代码应使用 Indexes。
	Index string

	// Indexes holds index.md content keyed by directory-relative path ("." = root,
	// "tables" = tables/index.md). OKF §8 允许 index.md 出现在任意目录。
	Indexes map[string]string

	// Logs holds log.md content keyed by directory-relative path. OKF §9 允许 log.md
	// 出现在任意目录。
	Logs map[string]string

	// OKFVersion is the version declared in bundle-root index.md frontmatter (§12).
	OKFVersion string

	// ParseErrors records non-reserved .md files with unparseable frontmatter.
	ParseErrors []ParseError

	// Config is the optional .okf.yaml configuration.
	Config *config.Config
}

// New creates an empty Bundle.
func New(rootPath string) *Bundle {
	return &Bundle{
		RootPath: rootPath,
		Concepts: make(map[string]*Concept),
		Indexes:  make(map[string]string),
		Logs:     make(map[string]string),
	}
}

// setIndex records an index.md file's content for a directory. The root (".")
// also populates the legacy Index field and extracts OKFVersion from frontmatter.
func (b *Bundle) setIndex(dir, content string) {
	b.Indexes[dir] = content
	if dir == "." {
		b.Index = content
	}
}

// HasIndex reports whether the bundle has any index.md file.
func (b *Bundle) HasIndex() bool {
	return len(b.Indexes) > 0
}

// Get looks up a concept by its ID.
func (b *Bundle) Get(id string) *Concept {
	return b.Concepts[id]
}

// Search performs a simple keyword search over concept titles, descriptions,
// types, tags and body content.
func (b *Bundle) Search(query string) []*Concept {
	q := strings.ToLower(query)
	var results []*Concept
	for _, c := range b.Concepts {
		score := 0
		if c.Meta != nil {
			if strings.Contains(strings.ToLower(c.Meta.Title), q) {
				score += 10
			}
			if strings.Contains(strings.ToLower(c.Meta.Type), q) {
				score += 8
			}
			if strings.Contains(strings.ToLower(c.Meta.Description), q) {
				score += 5
			}
			for _, tag := range c.Meta.Tags {
				if strings.Contains(strings.ToLower(tag), q) {
					score += 4
				}
			}
		}
		if strings.Contains(strings.ToLower(c.Body), q) {
			score += 2
		}
		if score > 0 {
			results = append(results, c)
		}
	}
	return results
}

// FilterByType returns concepts matching the given type.
func (b *Bundle) FilterByType(typ string) []*Concept {
	var results []*Concept
	for _, c := range b.Concepts {
		if c.Meta != nil && c.Meta.Type == typ {
			results = append(results, c)
		}
	}
	return results
}

// FilterByTag returns concepts that have the given tag.
func (b *Bundle) FilterByTag(tag string) []*Concept {
	var results []*Concept
	for _, c := range b.Concepts {
		if c.Meta == nil {
			continue
		}
		for _, t := range c.Meta.Tags {
			if t == tag {
				results = append(results, c)
				break
			}
		}
	}
	return results
}

// FilterByTrustTier returns concepts at or above the given trust tier.
func (b *Bundle) FilterByTrustTier(tier frontmatter.TrustTier) []*Concept {
	var results []*Concept
	for _, c := range b.Concepts {
		if c.TrustTier() == tier {
			results = append(results, c)
		}
	}
	return results
}

// StaleAndDeprecated returns stale and deprecated concepts.
func (b *Bundle) StaleAndDeprecated() (stale, deprecated []*Concept) {
	for _, c := range b.Concepts {
		if c.IsDeprecated() {
			deprecated = append(deprecated, c)
		} else if c.IsStale() {
			stale = append(stale, c)
		}
	}
	return
}

// IDFromPath converts an absolute file path to a Concept ID.
func (b *Bundle) IDFromPath(absPath string) string {
	rel, _ := filepath.Rel(b.RootPath, absPath)
	rel = filepath.ToSlash(rel)
	return strings.TrimSuffix(rel, ".md")
}
