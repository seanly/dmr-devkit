package service

import (
	"path/filepath"
	"testing"

	"github.com/seanly/dmr-devkit/okf/bundle"
	"github.com/seanly/dmr-devkit/okf/search"
	"github.com/seanly/dmr-devkit/okf/vcs"
)

// newServiceWithVCS builds a Service over a temp bundle with FTS + git enabled.
func newServiceWithVCS(t *testing.T) (*Service, string) {
	t.Helper()
	_, root := writeTempBundle(t)
	loader := bundle.NewLoader()
	b, err := loader.Load(root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	w := bundle.NewWriter(root)
	idx, err := search.Open(filepath.Join(root, ".okf", "index.db"), b.Concepts)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	g := vcs.New(root, "", "")
	if err := g.Init(); err != nil {
		t.Fatalf("vcs init: %v", err)
	}
	return NewWithVCS(b, w, idx, g), root
}

func TestVCSDisabledByDefault(t *testing.T) {
	svc, _ := writeTempBundle(t) // New(b): no vcs
	if svc.VCS() != nil {
		t.Fatal("VCS should be nil by default")
	}
	if _, err := svc.Commit("x"); err == nil {
		t.Error("Commit should error when vcs disabled")
	}
	if _, err := svc.GitLog("", 0); err == nil {
		t.Error("GitLog should error when vcs disabled")
	}
}

func TestRevertResyncsMemoryAndFTS(t *testing.T) {
	svc, _ := newServiceWithVCS(t)

	// Commit the seed state.
	mustCommit(t, svc, "seed")

	// Update concept a to "v2" and commit.
	if _, err := svc.UpdateConcept("concepts/a", "Body of A v2."); err != nil {
		t.Fatalf("update: %v", err)
	}
	mustCommit(t, svc, "edit a")

	// Confirm memory + FTS see v2.
	if got := bodyOf(t, svc, "concepts/a"); got != "Body of A v2." {
		t.Fatalf("pre-revert body = %q, want v2", got)
	}
	if hits := svc.SearchConcepts("v2"); len(hits) == 0 {
		t.Fatal("expected FTS hit for v2 before revert")
	}

	// Revert the last commit ("edit a") → memory + FTS must resync to v1.
	logs, err := svc.GitLog("concepts/a", 0)
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}
	if len(logs) < 1 {
		t.Fatal("expected at least one commit for concepts/a")
	}
	lastSha := logs[0].Sha
	if _, err := svc.Revert(lastSha); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	if got := bodyOf(t, svc, "concepts/a"); got != "Body of A." {
		t.Fatalf("post-revert body = %q, want v1", got)
	}
	// FTS must no longer match v2 (resynced), but still match the seed body.
	if hits := svc.SearchConcepts("v2"); len(hits) != 0 {
		t.Errorf("expected no FTS hit for v2 after revert, got %d", len(hits))
	}
	if hits := svc.SearchConcepts("Body"); len(hits) == 0 {
		t.Error("expected FTS hit for seed body after revert")
	}
}

func TestRestoreResyncsMemory(t *testing.T) {
	svc, _ := newServiceWithVCS(t)
	mustCommit(t, svc, "seed")
	logs, err := svc.GitLog("concepts/a", 0)
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}
	seedSha := logs[0].Sha

	// Edit and commit.
	if _, err := svc.UpdateConcept("concepts/a", "Body of A v2."); err != nil {
		t.Fatalf("update: %v", err)
	}
	mustCommit(t, svc, "edit a")

	// Restore the concept file to the seed commit.
	if _, err := svc.Restore("concepts/a", seedSha); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := bodyOf(t, svc, "concepts/a"); got != "Body of A." {
		t.Fatalf("post-restore body = %q, want v1", got)
	}
}

func mustCommit(t *testing.T, svc *Service, msg string) {
	t.Helper()
	if _, err := svc.Commit(msg); err != nil {
		t.Fatalf("Commit %q: %v", msg, err)
	}
}

func bodyOf(t *testing.T, svc *Service, id string) string {
	t.Helper()
	d, err := svc.GetConcept(id)
	if err != nil {
		t.Fatalf("GetConcept %s: %v", id, err)
	}
	b, _ := d["body"].(string)
	return b
}
