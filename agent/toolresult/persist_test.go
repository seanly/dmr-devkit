package toolresult

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistAndBuildMessage_WritesUnderWorkspace(t *testing.T) {
	ws := t.TempDir()
	p := Policy{Workspace: ws, PreviewRunes: 100}
	msg, _, err := p.persistAndBuildMessage(ws, "main", "call-1", strings.Repeat("x", 600))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, PersistedOutputTag) {
		t.Fatalf("expected persisted tag, got %q", msg)
	}
	rel := filepath.Join(".dmr", "tool-results", "main", "call-1.txt")
	full := filepath.Join(ws, rel)
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("file missing: %v", err)
	}
}

func TestPersistAndBuildMessage_EEXISTIdempotent(t *testing.T) {
	ws := t.TempDir()
	p := Policy{Workspace: ws}
	first := "original-body"
	_, _, err := p.persistAndBuildMessage(ws, "t", "id1", first)
	if err != nil {
		t.Fatal(err)
	}
	msg2, _, err := p.persistAndBuildMessage(ws, "t", "id1", "different-body")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg2, "original-body") {
		t.Fatalf("EEXIST should reuse file content for preview, got %q", msg2)
	}
}

func TestProcessNew_NoWorkspaceFallback(t *testing.T) {
	m := NewManager(Policy{DefaultMaxChars: 100})
	out := m.ProcessNew(50, "main", "c1", "shell", strings.Repeat("a", 200))
	if strings.Contains(out, PersistedOutputTag) {
		t.Fatal("expected truncate fallback without workspace")
	}
	if !strings.Contains(out, "omitted") {
		t.Fatalf("expected truncation hint, got %q", out)
	}
}

func TestProcessNew_SkipsFsRead(t *testing.T) {
	ws := t.TempDir()
	m := NewManager(Policy{Workspace: ws, DefaultMaxChars: 10})
	big := strings.Repeat("z", 500)
	out := m.ProcessNew(10, "main", "c1", "fsRead", big)
	if strings.Contains(out, PersistedOutputTag) {
		t.Fatal("fsRead should never externalize")
	}
	if out != big {
		t.Fatalf("expected full content, got len %d", len(out))
	}
}

func TestPersistDirUnderWorkspace_RejectDotDot(t *testing.T) {
	ws := t.TempDir()
	_, _, err := persistDirUnderWorkspace(ws, "..", "main")
	if err == nil {
		t.Fatal("expected error for .. subdir")
	}
	_, _, err = persistDirUnderWorkspace(ws, "foo/../bar", "main")
	if err == nil {
		t.Fatal("expected error for foo/../bar subdir")
	}
}

func TestPersistDirUnderWorkspace_SanitizeTraversalTape(t *testing.T) {
	ws := t.TempDir()
	abs, rel, err := persistDirUnderWorkspace(ws, ".dmr/tool-results", "../escape")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasPrefix(rel, "..") {
		t.Fatalf("relative path must not escape workspace: %q", rel)
	}
	if !strings.HasPrefix(abs, ws) {
		t.Fatalf("abs dir must stay under workspace: %q", abs)
	}
}

func TestPersistAndBuildMessage_SanitizeToolCallID(t *testing.T) {
	ws := t.TempDir()
	p := Policy{Workspace: ws}
	_, _, err := p.persistAndBuildMessage(ws, "main", "../etc/passwd", "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// sanitized filename should be __etc_passwd.txt, not traversing
	badPath := filepath.Join(ws, "etc", "passwd.txt")
	if _, err := os.Stat(badPath); err == nil {
		t.Fatal("toolCallID traversal should have been sanitized")
	}
}
