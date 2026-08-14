package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/seanly/dmr-devkit/okf/frontmatter"
)

// Writer is the filesystem write-back layer for an OKF bundle. It renders
// concept documents (via frontmatter.RenderDocument) and writes them
// atomically, enforces path safety (no traversal outside the bundle root),
// and never touches the in-memory Bundle — memory synchronization is the
// Service layer's job.
type Writer struct {
	root string
}

// NewWriter creates a Writer bound to the given bundle root.
func NewWriter(root string) *Writer {
	return &Writer{root: root}
}

// WriteConcept renders meta+body into a full OKF document and writes it
// atomically to <root>/<id>.md, creating parent directories as needed.
// The id is bundle-relative without the .md suffix (e.g. "tables/orders").
func (w *Writer) WriteConcept(id string, meta *frontmatter.Meta, body string) error {
	abs, err := w.safePath(id)
	if err != nil {
		return err
	}
	doc, err := frontmatter.RenderDocument(meta, body)
	if err != nil {
		return fmt.Errorf("render %s: %w", id, err)
	}
	return atomicWrite(abs, doc)
}

// Delete removes the concept file at <root>/<id>.md. Missing files are not
// an error (idempotent delete).
func (w *Writer) Delete(id string) error {
	abs, err := w.safePath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete %s: %w", id, err)
	}
	return nil
}

// AppendLog appends an entry to the log.md in the given directory (". " for
// the bundle root), creating the file if absent. OKF §9 allows log.md in any
// directory.
func (w *Writer) AppendLog(dir, entry string) error {
	if dir == "" {
		dir = "."
	}
	rel := filepath.ToSlash(filepath.Join(dir, "log.md"))
	abs, err := w.safePathRel(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()
	if !strings.HasSuffix(entry, "\n") {
		entry += "\n"
	}
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("write log: %w", err)
	}
	return nil
}

// safePath resolves a concept id (bundle-relative, no .md) to an absolute file
// path under the bundle root, rejecting ids that escape the root.
func (w *Writer) safePath(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("empty concept id")
	}
	return w.safePathRel(id + ".md")
}

// safePathRel resolves a bundle-relative path to an absolute path under root,
// rejecting any path that escapes the bundle root via ".." segments.
func (w *Writer) safePathRel(rel string) (string, error) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", fmt.Errorf("path escapes bundle root: %s", rel)
		}
	}
	abs := filepath.Join(w.root, filepath.FromSlash(rel))
	// Final guard: ensure the resolved path is within root.
	resolved, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	rootAbs, _ := filepath.Abs(w.root)
	if !strings.HasPrefix(resolved+string(filepath.Separator), rootAbs+string(filepath.Separator)) && resolved != rootAbs {
		return "", fmt.Errorf("path escapes bundle root: %s", rel)
	}
	return resolved, nil
}

// atomicWrite writes content to path via a temp file in the same directory,
// then renames over the target. Parent directories are created first.
func atomicWrite(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".okf-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if rename succeeded
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
