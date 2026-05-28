package toolresult

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// sanitizePathSegment avoids path traversal / odd tape names when writing under workspace.
func sanitizePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '<', '>', '|', '?', '*', '\x00':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "_"
	}
	return out
}

// persistAndBuildMessage writes full UTF-8 text under workspace and builds the LLM-visible block.
func (p *Policy) persistAndBuildMessage(workspace, tape, toolCallID, content string) (string, map[string]string, error) {
	if workspace == "" {
		return "", nil, fs.ErrPermission
	}
	dir, relBase, err := persistDirUnderWorkspace(workspace, p.persistSubdir(), tape)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	filename := sanitizePathSegment(toolCallID) + ".txt"
	full := filepath.Join(dir, filename)
	rel := filepath.ToSlash(filepath.Join(relBase, filename))

	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			existing, readErr := os.ReadFile(full)
			if readErr != nil {
				return "", nil, readErr
			}
			return p.buildPersistedMessage(rel, string(existing), len(existing))
		}
		return "", nil, err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(full)
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		return "", nil, err
	}
	return p.buildPersistedMessage(rel, content, len(content))
}

// persistDirUnderWorkspace returns the tape directory and its path relative to workspace.
func persistDirUnderWorkspace(workspace, subdir, tape string) (absDir string, relDir string, err error) {
	if subdir == "" {
		subdir = DefaultPersistSubdir
	}
	parts := []string{workspace}
	for _, seg := range strings.FieldsFunc(subdir, func(r rune) bool { return r == '/' || r == '\\' }) {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			return "", "", fmt.Errorf("toolresult: invalid persist subdir %q", subdir)
		}
		parts = append(parts, sanitizePathSegment(seg))
	}
	parts = append(parts, sanitizePathSegment(tape))
	absDir = filepath.Join(parts...)
	relDir, err = filepath.Rel(workspace, absDir)
	if err != nil || strings.HasPrefix(relDir, "..") {
		return "", "", fmt.Errorf("toolresult: persist dir outside workspace")
	}
	return absDir, relDir, nil
}

func (p *Policy) buildPersistedMessage(relPath string, fullContent string, byteSize int) (string, map[string]string, error) {
	pr := p.effectivePreviewRunes()
	preview, hasMore := GeneratePreview(fullContent, pr)
	var b strings.Builder
	b.WriteString(PersistedOutputTag)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Output too large (%s). Full output saved to: %s\n\n", humanBytes(byteSize), relPath))
	b.WriteString(fmt.Sprintf("Preview (first %d runes):\n", pr))
	b.WriteString(preview)
	if hasMore {
		b.WriteString("\n...\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(PersistedOutputCloseTag)
	s := b.String()
	meta := map[string]string{
		"path":         relPath,
		"originalSize": fmt.Sprintf("%d", byteSize),
	}
	return s, meta, nil
}
