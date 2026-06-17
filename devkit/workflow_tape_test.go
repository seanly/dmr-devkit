package devkit

import (
	"testing"

	"github.com/seanly/dmr-devkit/workflow"
)

func TestResolveWorkflowTape(t *testing.T) {
	wctx := workflow.NewContext()
	wctx.Metadata["run_id"] = "abc123"
	base := "workflow/demo/default/stage1:agent1"
	got := resolveWorkflowTape(base, wctx)
	want := "workflow/demo/abc123/stage1:agent1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if resolveWorkflowTape(base, nil) != base {
		t.Fatal("nil wctx should preserve base")
	}
}
