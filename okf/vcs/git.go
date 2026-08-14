// Package vcs provides optional Git versioning for an OKF bundle directory.
//
// It shells out to the system `git` CLI (no go-git dependency) so the bundle
// history stays human-inspectable with plain `git log`. All operations target
// a single bundle root via `git -C <root>`. The type is optional by design: a
// nil *Git means versioning is disabled and the Service layer degrades to the
// un-versioned write path.
package vcs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitignoreContent is written to <root>/.gitignore on Init. The .okf/ entry is
// critical: the FTS5 index lives at <root>/.okf/index.db and is rewritten on
// every mutation — tracking it would bloat history with binary noise and make
// diffs meaningless. .okf-tmp-* are the Writer's atomic-write temp files.
const gitignoreContent = ".okf/\n.okf-tmp-*\n"

// DefaultAuthor / DefaultEmail are used when no identity is configured.
const (
	DefaultAuthor = "agent:okf-playground"
	DefaultEmail  = "okf-playground@local"
)

// Commit is a parsed git commit entry.
type Commit struct {
	Sha     string
	Author  string
	At      time.Time
	Message string
	Files   []string // paths changed by this commit (bundle-relative)
}

// Git wraps git CLI operations over a single bundle root.
type Git struct {
	root   string
	author string
	email  string
}

// New creates a Git versioning handle. author/email are stamped into the local
// git config by Init; empty values fall back to the defaults.
func New(root, author, email string) *Git {
	if author == "" {
		author = DefaultAuthor
	}
	if email == "" {
		email = DefaultEmail
	}
	return &Git{root: root, author: author, email: email}
}

// Available reports whether the git CLI is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Root returns the bundle root this handle operates on.
func (g *Git) Root() string { return g.root }

// Init initializes the repo: `git init`, writes .gitignore if absent, and sets
// local user.name/user.email. Idempotent — safe to call on an existing repo.
func (g *Git) Init() error {
	if _, err := g.run("init"); err != nil {
		return fmt.Errorf("vcs: git init: %w", err)
	}
	// .gitignore (do not overwrite an existing one — the user may have extended it).
	giPath := filepath.Join(g.root, ".gitignore")
	if _, err := os.Stat(giPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(giPath, []byte(gitignoreContent), 0644); err != nil {
			return fmt.Errorf("vcs: write .gitignore: %w", err)
		}
	}
	if _, err := g.run("config", "user.name", g.author); err != nil {
		return fmt.Errorf("vcs: config user.name: %w", err)
	}
	if _, err := g.run("config", "user.email", g.email); err != nil {
		return fmt.Errorf("vcs: config user.email: %w", err)
	}
	return nil
}

// CommitIfDirty stages all changes (`git add -A`) and commits them if there is
// anything staged that differs from HEAD. Returns committed=false (no error)
// when the working tree is clean — a no-op commit is never created.
func (g *Git) CommitIfDirty(msg string) (bool, error) {
	if _, err := g.run("add", "-A"); err != nil {
		return false, fmt.Errorf("vcs: git add: %w", err)
	}
	// Exit code 0 = nothing staged differs from HEAD; 1 = there are staged changes.
	if err := g.runFailOK("diff", "--cached", "--quiet", "--exit-code"); err == nil {
		return false, nil // clean
	}
	if _, err := g.run("commit", "-m", msg); err != nil {
		return false, fmt.Errorf("vcs: git commit: %w", err)
	}
	return true, nil
}

// Log returns commit history, optionally restricted to a single path (a
// bundle-relative file path such as "tables/orders.md"). limit<=0 means no cap.
// Most recent first.
func (g *Git) Log(path string, limit int) ([]Commit, error) {
	args := []string{"log", "--name-only", "--pretty=format:%x1e%H%x1f%an%x1f%aI%x1f%s"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := g.run(args...)
	if err != nil {
		return nil, fmt.Errorf("vcs: git log: %w", err)
	}
	return parseLog(out), nil
}

// Diff returns a textual diff. If ref is empty it shows uncommitted working-tree
// changes; otherwise it shows the changes introduced by that commit. path
// optionally restricts to a single file.
func (g *Git) Diff(path, ref string) (string, error) {
	args := []string{"diff"}
	if ref != "" {
		args = append(args, ref+"^", ref)
	}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := g.run(args...)
	if err != nil {
		// Root commit has no parent; fall back to showing the commit itself.
		if ref != "" {
			if out2, err2 := g.run("show", "--format=", ref); err2 == nil {
				return out2, nil
			}
		}
		return "", fmt.Errorf("vcs: git diff: %w", err)
	}
	return out, nil
}

// Revert creates a new commit that undoes the given commit (`git revert
// --no-edit`). History is preserved.
func (g *Git) Revert(ref string) error {
	if _, err := g.run("revert", "--no-edit", ref); err != nil {
		return fmt.Errorf("vcs: git revert %s: %w", ref, err)
	}
	return nil
}

// Restore restores a single file path to its state at ref (`git checkout <ref>
// -- <path>`), leaving history untouched. path is bundle-relative (e.g.
// "tables/orders.md").
func (g *Git) Restore(path, ref string) error {
	if path == "" {
		return errors.New("vcs: restore requires a path")
	}
	if _, err := g.run("checkout", ref, "--", path); err != nil {
		return fmt.Errorf("vcs: git checkout %s -- %s: %w", ref, path, err)
	}
	return nil
}

// ChangedFiles returns the bundle-relative paths a commit touched. Used after
// Revert to reload the affected concepts into memory.
func (g *Git) ChangedFiles(ref string) ([]string, error) {
	out, err := g.run("diff-tree", "--no-commit-id", "--name-only", "-r", "--root", ref)
	if err != nil {
		return nil, fmt.Errorf("vcs: changed files for %s: %w", ref, err)
	}
	var files []string
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			files = append(files, ln)
		}
	}
	return files, nil
}

// run executes `git -C <root> <args...>` and returns trimmed stdout. A non-zero
// exit is reported as an *exec.ExitError-wrapped error.
func (g *Git) run(args ...string) (string, error) {
	full := append([]string{"-C", g.root}, args...)
	cmd := exec.Command("git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return stdout.String(), fmt.Errorf("%w: %s", ee, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// runFailOK runs git and returns nil on exit 0, a non-nil error otherwise. Used
// for `--quiet --exit-code` probes whose non-zero exit is meaningful, not an error.
func (g *Git) runFailOK(args ...string) error {
	full := append([]string{"-C", g.root}, args...)
	cmd := exec.Command("git", full...)
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}
	return cmd.Run()
}

// parseLog parses the %x1e/%x1f-delimited output of Log into Commit records.
func parseLog(out string) []Commit {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		// Fields are separated by \x1f; file paths follow on subsequent lines.
		parts := strings.SplitN(rec, "\x1f", 4)
		if len(parts) < 4 {
			continue
		}
		c := Commit{
			Sha:     parts[0],
			Author:  parts[1],
			Message: parts[3],
		}
		if t, err := time.Parse(time.RFC3339, parts[2]); err == nil {
			c.At = t
		}
		// Remaining newline-separated lines (after the 4th field's trailing newline)
		// are the changed file paths.
		rest := parts[3]
		if idx := strings.Index(rest, "\n"); idx >= 0 {
			c.Message = rest[:idx]
			for _, f := range strings.Split(rest[idx+1:], "\n") {
				f = strings.TrimSpace(f)
				if f != "" {
					c.Files = append(c.Files, f)
				}
			}
		}
		commits = append(commits, c)
	}
	return commits
}
