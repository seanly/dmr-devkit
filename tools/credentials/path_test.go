package credentials

import (
	"context"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

func TestResolveWorkspacePath_AbsoluteWithinWorkspace(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	got, err := resolveWorkspacePath(ctx, "/workspace/sub/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/workspace/sub/file.txt"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveWorkspacePath_AbsoluteOutsideWorkspace(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	_, err := resolveWorkspacePath(ctx, "/etc/passwd")
	if err == nil {
		t.Fatal("resolveWorkspacePath should reject absolute paths outside workspace")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveWorkspacePath_TraversalBlocked(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	cases := []string{
		"../etc/passwd",
		"sub/../../../etc/passwd",
		"foo/../../bar",
	}
	for _, raw := range cases {
		_, err := resolveWorkspacePath(ctx, raw)
		if err == nil {
			t.Fatalf("resolveWorkspacePath(%q) should block traversal", raw)
		}
		if !strings.Contains(err.Error(), "escapes workspace") {
			t.Fatalf("resolveWorkspacePath(%q) unexpected error: %v", raw, err)
		}
	}
}

func TestResolveWorkspacePath_RelativeAllowed(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = "/workspace"

	got, err := resolveWorkspacePath(ctx, "sub/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/workspace/sub/file.txt"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveWorkspacePath_NoWorkspace(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	_, err := resolveWorkspacePath(ctx, "file.txt")
	if err == nil {
		t.Fatal("expected error when workspace is missing")
	}
	if !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}
