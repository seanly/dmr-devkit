package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

func TestResolvePath_AbsoluteWithinWorkspace(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	got, err := resolvePath(ctx, "/workspace/sub/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/workspace/sub/file.txt"
	if got != want {
		t.Errorf("resolvePath(abs within ws) = %q, want %q", got, want)
	}
}

func TestResolvePath_AbsoluteOutsideWorkspace(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	got, err := resolvePath(ctx, "/etc/hosts")
	if err != nil {
		t.Fatalf("absolute paths outside workspace should be allowed by fs plugin (policy decides): %v", err)
	}
	want := "/etc/hosts"
	if got != want {
		t.Errorf("resolvePath(abs outside ws) = %q, want %q", got, want)
	}
}

func TestResolvePath_RelativeWithWorkspace(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	got, err := resolvePath(ctx, "sub/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/workspace", "sub/file.txt")
	if got != want {
		t.Errorf("resolvePath(rel+ws) = %q, want %q", got, want)
	}
}

func TestResolvePath_RelativeNoWorkspace(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")

	_, err := resolvePath(ctx, "file.txt")
	if err == nil {
		t.Fatal("resolvePath should require a workspace")
	}
	if !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolvePath_TraversalBlocked(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	cases := []string{
		"../etc/passwd",
		"sub/../../../etc/passwd",
		"foo/../../bar",
	}
	for _, raw := range cases {
		_, err := resolvePath(ctx, raw)
		if err == nil {
			t.Fatalf("resolvePath(%q) should block traversal, got nil", raw)
		}
		if !strings.Contains(err.Error(), "escapes workspace") {
			t.Fatalf("resolvePath(%q) unexpected error: %v", raw, err)
		}
	}
}

func TestResolvePath_SymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	ws := filepath.Join(tmpDir, "workspace")
	os.MkdirAll(ws, 0o755)

	secretFile := filepath.Join(tmpDir, "secret.txt")
	os.WriteFile(secretFile, []byte("sensitive info"), 0o600)

	// Create a symlink inside workspace pointing outside
	linkPath := filepath.Join(ws, "link_to_secret")
	err := os.Symlink(secretFile, linkPath)
	if err != nil {
		t.Skip("symlinks not supported on this platform or insufficient permissions")
	}

	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = ws

	// Attempt to resolve the link
	_, err = resolvePath(ctx, "link_to_secret")
	if err == nil {
		t.Fatal("resolvePath should block symlink escape")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteHandler_CreatesFileWithRestrictedPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = tmpDir

	_, err := writeHandler(ctx, map[string]any{
		"path":    "subdir/test.txt",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("writeHandler error: %v", err)
	}

	fullPath := filepath.Join(tmpDir, "subdir", "test.txt")
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Fatalf("file perm = %o, want 0o600", mode)
	}
}

func TestEditHandler_StartBeyondFileLength(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "doc.md")
	if err := os.WriteFile(path, []byte("line one\nline two\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = tmpDir

	_, err := editHandler(ctx, map[string]any{
		"path":  "doc.md",
		"old":   "---",
		"new":   "replaced",
		"start": float64(330),
	})
	if err == nil {
		t.Fatal("editHandler should return error when old text not found after clamped start")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTools(t *testing.T) {
	tools := Tools()
	if len(tools) != 6 {
		t.Fatalf("Tools() len = %d, want 6", len(tools))
	}
}
