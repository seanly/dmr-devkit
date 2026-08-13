package script

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/tool"
)

// writeScript creates an executable file under dir with the given content.
func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// shShebang returns a portable shebang. /bin/sh is available on all supported
// platforms (darwin/linux) and avoids a bash dependency in tests.
func shShebang(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("script tools require a Unix shell")
	}
	return "#!/bin/sh"
}

func TestSpec_TimeoutAndGroup(t *testing.T) {
	c := Spec{Name: "x", Parameters: map[string]any{"type": "object"}, Timeout: "2s", Group: "core"}
	if got := c.timeout(); got != 2*time.Second {
		t.Errorf("timeout = %v, want 2s", got)
	}
	if got := c.group(); got != tool.ToolGroupCore {
		t.Errorf("group = %v, want core", got)
	}

	bad := Spec{Name: "x", Parameters: map[string]any{"type": "object"}, Timeout: "nope"}
	if got := bad.timeout(); got != defaultTimeout {
		t.Errorf("bad timeout fallback = %v, want %v", got, defaultTimeout)
	}
	empty := Spec{Name: "x", Parameters: map[string]any{"type": "object"}}
	if got := empty.group(); got != tool.ToolGroupExtended {
		t.Errorf("default group = %v, want extended", got)
	}
}

func TestDiscoverScripts_FiltersAndOrder(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	// Valid tools, unordered by name but ordered by numeric prefix.
	writeScript(t, dir, "002-b.sh", sh+"\ntrue\n")
	writeScript(t, dir, "001-a.sh", sh+"\ntrue\n")
	// Hidden -> skipped.
	writeScript(t, dir, ".hidden.sh", sh+"\ntrue\n")
	// Data extension -> skipped.
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-executable -> skipped.
	if err := os.WriteFile(filepath.Join(dir, "notexec.sh"), []byte(sh+"\ntrue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// lib/ directory with an executable -> skipped entirely.
	libDir := filepath.Join(dir, "lib")
	if err := os.Mkdir(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, libDir, "helper.sh", sh+"\ntrue\n")

	paths, err := discoverScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths %v, want 2", len(paths), paths)
	}
	// Sorted by path => 001-a.sh before 002-b.sh.
	base0 := filepath.Base(paths[0])
	base1 := filepath.Base(paths[1])
	if base0 != "001-a.sh" || base1 != "002-b.sh" {
		t.Errorf("order = %s, %s; want 001-a.sh, 002-b.sh", base0, base1)
	}
}

func TestFetchScriptConfig_OKAndErrors(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()

	// Valid config.
	good := writeScript(t, dir, "good.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"good","description":"d","parameters":{"type":"object"},"timeout":"5s"}'
  exit 0
fi
echo '{}' > "$TOOL_RESULT_PATH"
`)
	spec, err := fetchScriptConfig(context.Background(), good)
	if err != nil {
		t.Fatalf("fetchScriptConfig: %v", err)
	}
	if spec.Name != "good" {
		t.Errorf("name = %q", spec.Name)
	}

	// Non-JSON stdout.
	badJSON := writeScript(t, dir, "badjson.sh", sh+`
if [ "$1" = "--config" ]; then echo 'not json'; exit 0; fi
`)
	if _, err := fetchScriptConfig(context.Background(), badJSON); err == nil {
		t.Error("expected error for non-JSON config, got nil")
	}

	// Non-zero exit on --config.
	badExit := writeScript(t, dir, "badexit.sh", sh+`
if [ "$1" = "--config" ]; then echo 'boom' >&2; exit 3; fi
`)
	if _, err := fetchScriptConfig(context.Background(), badExit); err == nil {
		t.Error("expected error for non-zero --config exit, got nil")
	}
}

func TestLoad_DedupeAndSkip(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	dupCfg := `{"name":"dup","description":"d","parameters":{"type":"object"}}`
	writeScript(t, dir, "001.sh", sh+"\nif [ \"$1\" = \"--config\" ]; then printf '%s' '"+dupCfg+"'; exit 0; fi\necho '{}' > \"$TOOL_RESULT_PATH\"\n")
	// Same name -> deduped.
	writeScript(t, dir, "002.sh", sh+"\nif [ \"$1\" = \"--config\" ]; then printf '%s' '"+dupCfg+"'; exit 0; fi\necho '{}' > \"$TOOL_RESULT_PATH\"\n")
	// Invalid -> skipped.
	writeScript(t, dir, "003.sh", sh+"\nif [ \"$1\" = \"--config\" ]; then echo 'nope'; exit 0; fi\n")

	tools, err := Load(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1 (deduped)", len(tools))
	}
	if tools[0].Spec.Name != "script_dup" {
		t.Errorf("name = %q, want script_dup", tools[0].Spec.Name)
	}
}

func TestRunScriptTool_Success(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	path := writeScript(t, dir, "ok.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"ok","description":"d","parameters":{"type":"object"},"timeout":"10s"}'
  exit 0
fi
echo '{"out":"ok","got":"'$TOOL_ARGS_PATH'"}' > "$TOOL_RESULT_PATH"
echo "log line" >&2
`)
	spec, err := fetchScriptConfig(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	tl := newScriptTool(path, spec, defaultNamePrefix, nil)

	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	res, err := tl.Handler(ctx, map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["out"] != "ok" {
		t.Errorf("result = %v", res)
	}
}

func TestRunScriptTool_NonZeroExit(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	path := writeScript(t, dir, "fail.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"fail","description":"d","parameters":{"type":"object"},"timeout":"10s"}'
  exit 0
fi
echo "something broke" >&2
exit 7
`)
	spec, _ := fetchScriptConfig(context.Background(), path)
	tl := newScriptTool(path, spec, defaultNamePrefix, nil)
	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	_, err := tl.Handler(ctx, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "non-zero") {
		t.Errorf("error = %v, want it to mention non-zero", err)
	}
}

func TestRunScriptTool_BadResultJSON(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	path := writeScript(t, dir, "badres.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"badres","description":"d","parameters":{"type":"object"},"timeout":"10s"}'
  exit 0
fi
echo 'not json' > "$TOOL_RESULT_PATH"
`)
	spec, _ := fetchScriptConfig(context.Background(), path)
	tl := newScriptTool(path, spec, defaultNamePrefix, nil)
	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	_, err := tl.Handler(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid result JSON") {
		t.Errorf("error = %v, want invalid result JSON", err)
	}
}

func TestRunScriptTool_Timeout(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	path := writeScript(t, dir, "slow.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"slow","description":"d","parameters":{"type":"object"},"timeout":"200ms"}'
  exit 0
fi
sleep 30
`)
	spec, _ := fetchScriptConfig(context.Background(), path)
	tl := newScriptTool(path, spec, defaultNamePrefix, nil)
	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	start := time.Now()
	_, err := tl.Handler(ctx, nil)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want timed out", err)
	}
	// Should return shortly after the 200ms deadline, not after the 30s sleep.
	if elapsed > 5*time.Second {
		t.Errorf("timeout took %v, expected ~200ms", elapsed)
	}
}

func TestRunScriptTool_EmptyResultIsNil(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	path := writeScript(t, dir, "empty.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"empty","description":"d","parameters":{"type":"object"},"timeout":"10s"}'
  exit 0
fi
: # leave TOOL_RESULT_PATH empty
`)
	spec, _ := fetchScriptConfig(context.Background(), path)
	tl := newScriptTool(path, spec, defaultNamePrefix, nil)
	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	res, err := tl.Handler(ctx, nil)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res != nil {
		t.Errorf("result = %v, want nil for empty result file", res)
	}
}

// TestLoad_RelativeDir guards against a regression where a relative tools dir
// caused run-mode exec to resolve the script path against the wrong directory
// (cmd.Dir was the script's dir, making a relative command path double-resolve).
// The script path must be absolute at exec time.
func TestLoad_RelativeDir(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	writeScript(t, dir, "rel.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"rel","description":"d","parameters":{"type":"object"},"timeout":"10s"}'
  exit 0
fi
echo '{"ok":true}' > "$TOOL_RESULT_PATH"
`)
	// Run from the parent of the tools dir, referring to it by basename only.
	t.Chdir(filepath.Dir(dir))
	relDir := filepath.Base(dir)
	tools, err := Load(context.Background(), relDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	ctx := tool.NewToolContext(context.Background(), "tape", "run")
	res, err := tools[0].Handler(ctx, nil)
	if err != nil {
		t.Fatalf("Handler with relative dir: %v", err)
	}
	if res.(map[string]any)["ok"] != true {
		t.Errorf("result = %v", res)
	}
}

// TestRunScriptTool_ToolEnvAndStaticEnv verifies the standardized TOOL_* context
// env vars and caller-supplied static env are injected into the run.
func TestRunScriptTool_ToolEnvAndStaticEnv(t *testing.T) {
	sh := shShebang(t)
	dir := t.TempDir()
	path := writeScript(t, dir, "env.sh", sh+`
if [ "$1" = "--config" ]; then
  echo '{"name":"env","description":"d","parameters":{"type":"object"},"timeout":"10s"}'
  exit 0
fi
printf '{"ws":"%s","tape":"%s","run":"%s","bundle":"%s"}' \
  "$TOOL_WORKSPACE" "$TOOL_TAPE" "$TOOL_RUN_ID" "$OKF_BUNDLE_ROOT" > "$TOOL_RESULT_PATH"
`)
	spec, _ := fetchScriptConfig(context.Background(), path)
	staticEnv := map[string]string{"OKF_BUNDLE_ROOT": "/bundle/root"}
	tl := newScriptTool(path, spec, defaultNamePrefix, staticEnv)

	ctx := tool.NewToolContext(context.Background(), "mytape", "myrun")
	ctx.Workspace = "/ws"
	res, err := tl.Handler(ctx, nil)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result not a map: %v", res)
	}
	if m["ws"] != "/ws" || m["tape"] != "mytape" || m["run"] != "myrun" {
		t.Errorf("TOOL_* env not injected: %v", m)
	}
	if m["bundle"] != "/bundle/root" {
		t.Errorf("static env not injected: %v", m)
	}
}
