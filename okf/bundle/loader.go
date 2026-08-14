package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/seanly/dmr-devkit/okf/config"
	"github.com/seanly/dmr-devkit/okf/frontmatter"
	"github.com/seanly/dmr-devkit/okf/markdown"
)

// defaultExcludeDirs are skipped during walking. Hidden directories (whose base
// name starts with ".") are also skipped, except the bundle root itself.
var defaultExcludeDirs = []string{".git", "node_modules", ".hg", ".svn"}

// Loader reads an OKF bundle from the filesystem into memory.
type Loader struct {
	Warnings    []string
	ExcludeDirs []string // nil ⇒ defaultExcludeDirs
}

// NewLoader creates a new Loader.
func NewLoader() *Loader {
	return &Loader{}
}

// Load recursively loads all .md files from the bundle directory.
func (l *Loader) Load(root string) (*Bundle, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve bundle path: %w", err)
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat bundle path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("bundle path is not a directory: %s", absRoot)
	}

	if l.ExcludeDirs == nil {
		l.ExcludeDirs = defaultExcludeDirs
	}

	b := New(absRoot)

	cfg, err := config.Load(absRoot)
	if err != nil {
		l.Warnings = append(l.Warnings, fmt.Sprintf("load config: %v", err))
	} else {
		b.Config = cfg
	}

	err = filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			l.Warnings = append(l.Warnings, fmt.Sprintf("walk error at %s: %v", path, err))
			return nil
		}
		if info.IsDir() {
			// Never skip the bundle root; skip hidden dirs and excluded dirs.
			if path != absRoot {
				base := filepath.Base(path)
				if strings.HasPrefix(base, ".") || contains(l.ExcludeDirs, base) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		rel, _ := filepath.Rel(absRoot, path)
		rel = filepath.ToSlash(rel)
		dir := dirRel(rel)
		base := filepath.Base(rel)

		// Reserved filenames (OKF §3.1)
		if base == "index.md" {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				l.Warnings = append(l.Warnings, fmt.Sprintf("read %s: %v", rel, rerr))
				return nil
			}
			b.setIndex(dir, string(data))
			// Bundle-root index.md may carry okf_version (§12).
			if rel == "index.md" {
				if res, ferr := frontmatter.Extract(string(data)); ferr == nil && res.Meta != nil {
					b.OKFVersion = res.Meta.OKFVersion
				}
			}
			return nil
		}
		if base == "log.md" {
			data, rerr := os.ReadFile(path)
			if rerr == nil {
				b.Logs[dir] = string(data)
			} else {
				l.Warnings = append(l.Warnings, fmt.Sprintf("read %s: %v", rel, rerr))
			}
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			l.Warnings = append(l.Warnings, fmt.Sprintf("read %s: %v", rel, err))
			return nil
		}

		res, err := frontmatter.Extract(string(data))
		if err != nil {
			// §11.1: every non-reserved .md must have parseable frontmatter.
			// Record for the validator to surface as an error.
			b.ParseErrors = append(b.ParseErrors, ParseError{
				FilePath: path,
				RelPath:  rel,
				Err:      err,
			})
			return nil
		}

		id := b.IDFromPath(path)
		links := markdown.ExtractLinks(res.Body)
		linkTargets := make([]string, 0, len(links))
		for _, lk := range links {
			resolved := resolveLink(rel, lk.URL)
			if resolved != "" {
				linkTargets = append(linkTargets, resolved)
			}
		}

		b.Concepts[id] = &Concept{
			ID:       id,
			Meta:     res.Meta,
			Body:     res.Body,
			Links:    linkTargets,
			FilePath: path,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return b, nil
}

// ResolveLinks extracts markdown links from body and resolves them to
// bundle-relative concept IDs, using the same resolution rules as the loader
// (absolute "/"-prefixed and relative links per OKF §6.1). relPath is the
// concept's bundle-relative path including the ".md" suffix (e.g.
// "tables/orders.md"). Exported so mutation paths can keep the in-memory
// Bundle's link graph consistent with a freshly written body.
func ResolveLinks(relPath, body string) []string {
	links := markdown.ExtractLinks(body)
	out := make([]string, 0, len(links))
	for _, lk := range links {
		if r := resolveLink(relPath, lk.URL); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// dirRel returns the directory portion of a bundle-relative path, with "" mapped
// to "." (root).
func dirRel(rel string) string {
	d := filepath.ToSlash(filepath.Dir(rel))
	if d == "" || d == "." {
		return "."
	}
	return d
}

// contains reports whether s is in list.
func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// resolveLink turns a markdown link URL into a bundle-relative concept ID.
// Supports absolute (bundle-relative, "/"-prefixed) and relative links per OKF §6.1.
// Directory links (trailing "/" or no ".md") resolve to "<dir>/index".
func resolveLink(fromPath, url string) string {
	url = strings.TrimSpace(url)
	// Strip fragment
	if idx := strings.Index(url, "#"); idx != -1 {
		url = url[:idx]
	}
	// Strip trailing slash (directory link)
	url = strings.TrimSuffix(url, "/")
	if url == "" {
		return ""
	}

	// Absolute (bundle-relative): begins with "/"
	if strings.HasPrefix(url, "/") {
		url = strings.TrimPrefix(url, "/")
		if !strings.HasSuffix(url, ".md") {
			url += "/index.md"
		}
		return strings.TrimSuffix(url, ".md")
	}

	// Relative
	if !strings.HasSuffix(url, ".md") {
		url += ".md"
	}
	fromDir := filepath.ToSlash(filepath.Dir(fromPath))
	resolved := filepath.ToSlash(filepath.Clean(filepath.Join(fromDir, url)))
	return strings.TrimSuffix(resolved, ".md")
}
