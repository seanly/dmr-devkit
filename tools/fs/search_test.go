package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

func TestPaginateOutput(t *testing.T) {
	out, truncated := paginateOutput("a\nb\nc\nd\ne", 1, 2)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(out, "b") || !strings.Contains(out, "c") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncated notice: %q", out)
	}
}

func TestBuildGrepArgs(t *testing.T) {
	args := buildGrepArgs(grepOptions{
		pattern:    "foo",
		searchPath: "/ws",
		outputMode: grepModeContent,
		glob:       "*.go",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--line-number", "-i", "--glob", "*.go", "foo", "/ws"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
}

func TestBuildGlobArgs(t *testing.T) {
	args := buildGlobArgs(globOptions{pattern: "**/*.go", searchPath: "/ws"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--files", "--glob", "**/*.go", "/ws"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
}

func TestListOneLevel(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600)

	out, err := listOneLevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dir/ sub") {
		t.Fatalf("missing sub dir: %q", out)
	}
	if !strings.Contains(out, "file a.txt") {
		t.Fatalf("missing a.txt: %q", out)
	}
}

func TestListHandler_NonRecursive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("1"), 0o600)

	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = dir

	out, err := listHandler(ctx, map[string]any{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "file x.txt") {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestGrepHandler_RequiresPattern(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = t.TempDir()
	_, err := grepHandler(ctx, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("expected pattern error, got %v", err)
	}
}

func TestGlobHandler_RequiresPattern(t *testing.T) {
	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = t.TempDir()
	_, err := globHandler(ctx, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("expected pattern error, got %v", err)
	}
}

func TestGrepIntegration(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not in PATH")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Hello() {}\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("Hello world\n"), 0o600)

	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = dir

	files, err := grepHandler(ctx, map[string]any{
		"pattern": "Hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	fs := files.(string)
	if !strings.Contains(fs, "main.go") || !strings.Contains(fs, "readme.txt") {
		t.Fatalf("files_with_matches: %q", fs)
	}

	content, err := grepHandler(ctx, map[string]any{
		"pattern":     "func Hello",
		"output_mode": "content",
		"glob":        "*.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	cs := content.(string)
	if !strings.Contains(cs, "func Hello") {
		t.Fatalf("content mode: %q", cs)
	}
	if strings.Contains(cs, "readme.txt") {
		t.Fatalf("glob filter should exclude txt: %q", cs)
	}
}

func TestGlobIntegration(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not in PATH")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0o600)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("text"), 0o600)

	ctx := tool.NewToolContext(context.Background(), "test", "")
	ctx.Workspace = dir

	out, err := globHandler(ctx, map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	s := out.(string)
	if !strings.Contains(s, "a.go") {
		t.Fatalf("expected a.go in %q", s)
	}
	if strings.Contains(s, "b.txt") {
		t.Fatalf("should not include b.txt: %q", s)
	}
}

func TestTools_IncludesSearchTools(t *testing.T) {
	tools := Tools()
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Spec.Name] = true
	}
	for _, want := range []string{"fsRead", "fsWrite", "fsEdit", "fsGrep", "fsGlob", "fsList"} {
		if !names[want] {
			t.Fatalf("missing tool %q", want)
		}
	}
}
