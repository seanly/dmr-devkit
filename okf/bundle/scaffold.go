package bundle

import (
	"os"
	"path/filepath"
)

// scaffoldFiles is the minimal OKF bundle skeleton written by Scaffold: a root
// index.md (progressive-disclosure entry), a starter concept, a changelog, and
// a .okf.yaml tightening config.
var scaffoldFiles = map[string]string{
	"index.md": `# My Knowledge Bundle

Welcome to this OKF knowledge bundle.

## Table of Contents

- [Overview](overview.md)
`,
	"overview.md": `---
type: concept
title: Overview
description: High-level overview of this knowledge bundle.
---

This bundle demonstrates the Open Knowledge Format (OKF) structure.
`,
	"log.md": `# Changelog

## 2026-08-12

- Initial bundle created.
`,
	".okf.yaml": `custom_types:
  - name: concept
    description: Generic knowledge concept
  - name: table
    required:
      - title
      - description
    description: Data table definition
  - name: guideline
    description: Process or quality guideline
  - name: Attested Computation
    required:
      - title
      - runtime
      - executor
    description: Attested computation logic

rules:
  require_index: true
  require_log: false
  warn_orphan: true
  warn_untagged: false
`,
}

// Scaffold writes a minimal OKF bundle skeleton (index.md, overview.md,
// log.md, .okf.yaml) into dir. The directory is created if it does not exist.
// Files that already exist are left untouched (skip), so Scaffold is safe to
// re-run over a partial bundle. It does not touch git — callers handle
// versioning separately.
func Scaffold(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, content := range scaffoldFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
