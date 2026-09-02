package toolresult

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	persist "github.com/seanly/dmr-devkit/agent/toolresult"
	"github.com/seanly/dmr-devkit/tool"
)

func TestToolsNames(t *testing.T) {
	got := Tools()
	if len(got) != 2 {
		t.Fatalf("Tools() len = %d, want 2", len(got))
	}
	if got[0].Spec.Name != ToolRead || got[1].Spec.Name != ToolGrep {
		t.Errorf("names = %s, %s", got[0].Spec.Name, got[1].Spec.Name)
	}
}

func TestResolvePathRejectsTraversal(t *testing.T) {
	ws := t.TempDir()
	persistDir := filepath.Join(ws, ".dmr", "tool-results", "tape")
	if err := os.MkdirAll(persistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(persistDir, "ok.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(ws), "secret.txt")
	if err := os.WriteFile(outside, []byte("leaked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	ctx.Workspace = ws
	o := options{persistSubdir: persist.DefaultPersistSubdir}

	for _, p := range []string{
		"../secret.txt",
		filepath.Join(".dmr", "tool-results", "..", "..", filepath.Base(outside)),
		outside,
	} {
		_, _, err := o.resolvePath(ctx, p, true)
		if err == nil {
			t.Errorf("path %q should be rejected", p)
		}
	}
}

func TestReadPagesLines(t *testing.T) {
	ws := t.TempDir()
	rel := filepath.Join(".dmr", "tool-results", "tape", "call-1.txt")
	abs := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i := 0; i < 10; i++ {
		body.WriteString("line-")
		body.WriteByte(byte('0' + i))
		body.WriteByte('\n')
	}
	if err := os.WriteFile(abs, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	ctx.Workspace = ws
	o := options{persistSubdir: persist.DefaultPersistSubdir}

	out, err := o.readHandler(ctx, map[string]any{
		"path":   filepath.ToSlash(rel),
		"offset": float64(3),
		"limit":  float64(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "lines 3-5 of 10") {
		t.Errorf("pagination header = %q", text)
	}
	if !strings.Contains(text, "line-3") || !strings.Contains(text, "line-4") {
		t.Errorf("missing paged lines: %q", text)
	}
	if strings.Contains(text, "line-2") || strings.Contains(text, "line-5") {
		t.Errorf("unexpected extra lines: %q", text)
	}
	if !strings.Contains(text, "offset=5") {
		t.Errorf("expected remaining-offset hint, got %q", text)
	}
}

func TestGrepMatchesAndHeadLimit(t *testing.T) {
	ws := t.TempDir()
	rel := filepath.Join(".dmr", "tool-results", "tape", "crash.txt")
	abs := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "ok\nunable to connect\nmetadata timeout\nok\nauthentication failed\n"
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	ctx.Workspace = ws
	o := options{persistSubdir: persist.DefaultPersistSubdir}

	out, err := o.grepHandler(ctx, map[string]any{
		"pattern":    "timeout|authentication",
		"head_limit": float64(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "metadata timeout") {
		t.Errorf("expected first match, got %q", text)
	}
	if strings.Contains(text, "authentication failed") {
		t.Errorf("head_limit=1 should not include second match: %q", text)
	}
	if !strings.Contains(text, "[truncated at 1 matches]") {
		t.Errorf("expected truncation marker, got %q", text)
	}
}

func TestReadRejectsMissingWorkspace(t *testing.T) {
	o := options{persistSubdir: persist.DefaultPersistSubdir}
	_, err := o.readHandler(tool.NewToolContext(context.Background(), "t", "r"), map[string]any{
		"path": ".dmr/tool-results/t/x.txt",
	})
	if err == nil {
		t.Fatal("expected error without workspace")
	}
}

func TestToolsWithCustomSubdir(t *testing.T) {
	ws := t.TempDir()
	rel := filepath.Join("custom", "persist", "tape", "out.txt")
	abs := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	ctx.Workspace = ws
	tools := ToolsWith("custom/persist")
	if len(tools) != 2 {
		t.Fatalf("ToolsWith len = %d", len(tools))
	}

	out, err := tools[0].Handler(ctx, map[string]any{"path": filepath.ToSlash(rel)})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "alpha") || !strings.Contains(text, "beta") {
		t.Errorf("custom subdir read = %q", text)
	}

	// Default persist tree must be out of scope.
	defaultRel := filepath.Join(".dmr", "tool-results", "tape", "other.txt")
	if err := os.MkdirAll(filepath.Join(ws, filepath.Dir(defaultRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, defaultRel), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = tools[0].Handler(ctx, map[string]any{"path": filepath.ToSlash(defaultRel)})
	if err == nil {
		t.Fatal("default persist path should be rejected when using custom subdir")
	}
}
