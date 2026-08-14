package vcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skipIfNoGit skips tests that require the git CLI.
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("git not on PATH")
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestInitAndCommitIfDirty(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	g := New(root, "", "")

	if err := g.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// .gitignore must exist and ignore .okf/.
	b, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !contains(string(b), ".okf/") {
		t.Errorf(".gitignore missing .okf/ entry: %q", string(b))
	}

	// Init writes .gitignore, so the tree is dirty: first commit captures it.
	committed, err := g.CommitIfDirty("init")
	if err != nil {
		t.Fatalf("CommitIfDirty init: %v", err)
	}
	if !committed {
		t.Errorf("expected a commit for the new .gitignore")
	}

	// Clean tree now: CommitIfDirty is a no-op (committed=false).
	committed, err = g.CommitIfDirty("nothing")
	if err != nil {
		t.Fatalf("CommitIfDirty clean: %v", err)
	}
	if committed {
		t.Errorf("expected no commit on clean tree")
	}

	// Write a concept and commit.
	writeFile(t, root, "tables/orders.md", "---\ntype: table\ntitle: Orders\n---\nbody\n")
	committed, err = g.CommitIfDirty("add orders")
	if err != nil {
		t.Fatalf("CommitIfDirty: %v", err)
	}
	if !committed {
		t.Errorf("expected a commit on dirty tree")
	}
}

func TestOkfDirNotTracked(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	g := New(root, "", "")
	if err := g.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	writeFile(t, root, "tables/orders.md", "x")
	// Simulate the FTS index file under .okf/.
	writeFile(t, root, ".okf/index.db", "binary noise")
	if _, err := g.CommitIfDirty("c"); err != nil {
		t.Fatalf("CommitIfDirty: %v", err)
	}
	files, err := g.ChangedFiles("HEAD")
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	for _, f := range files {
		if f == ".okf/index.db" {
			t.Errorf(".okf/index.db must not be tracked, got files=%v", files)
		}
	}
}

func TestLogDiffRevertRestore(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	g := New(root, "", "")
	if err := g.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Commit 1: create orders.
	writeFile(t, root, "tables/orders.md", "---\ntype: table\ntitle: Orders\n---\norders v1\n")
	mustCommit(t, g, "add orders")

	// Commit 2: edit orders.
	writeFile(t, root, "tables/orders.md", "---\ntype: table\ntitle: Orders\n---\norders v2\n")
	mustCommit(t, g, "edit orders")

	// Log: 2 commits, most recent first, each carrying the changed file.
	logs, err := g.Log("", 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(logs))
	}
	if logs[0].Message != "edit orders" {
		t.Errorf("first log message = %q, want %q", logs[0].Message, "edit orders")
	}
	if !contains(join(logs[0].Files), "tables/orders.md") {
		t.Errorf("first commit files = %v, want tables/orders.md", logs[0].Files)
	}

	// Log filtered by path still returns both.
	logsPath, err := g.Log("tables/orders.md", 0)
	if err != nil {
		t.Fatalf("Log path: %v", err)
	}
	if len(logsPath) != 2 {
		t.Errorf("path-filtered log = %d, want 2", len(logsPath))
	}

	// Diff of the last commit mentions v2.
	diff, err := g.Diff("tables/orders.md", logs[0].Sha)
	if err != nil {
		t.Fatalf("Diff ref: %v", err)
	}
	if !contains(diff, "orders v2") {
		t.Errorf("diff missing v2 content: %q", diff)
	}

	// Revert the last commit ("edit orders") → content back to v1.
	if err := g.Revert(logs[0].Sha); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "tables/orders.md"))
	if !contains(string(got), "orders v1") {
		t.Errorf("after revert content = %q, want v1", string(got))
	}

	// Restore the orders file to the v2 commit state.
	if err := g.Restore("tables/orders.md", logs[0].Sha); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got2, _ := os.ReadFile(filepath.Join(root, "tables/orders.md"))
	if !contains(string(got2), "orders v2") {
		t.Errorf("after restore content = %q, want v2", string(got2))
	}
}

func TestDiffWorkingTree(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	g := New(root, "", "")
	if err := g.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	writeFile(t, root, "a.md", "v1")
	mustCommit(t, g, "a")
	writeFile(t, root, "a.md", "v2-uncommitted")

	diff, err := g.Diff("a.md", "")
	if err != nil {
		t.Fatalf("Diff working tree: %v", err)
	}
	if !contains(diff, "v2-uncommitted") {
		t.Errorf("working-tree diff missing uncommitted change: %q", diff)
	}
}

func mustCommit(t *testing.T, g *Git, msg string) {
	t.Helper()
	if _, err := g.CommitIfDirty(msg); err != nil {
		t.Fatalf("CommitIfDirty %q: %v", msg, err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func join(ss []string) string { return strings.Join(ss, " ") }
