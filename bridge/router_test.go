package bridge

import (
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

func TestResolveWorkerID(t *testing.T) {
	tests := []struct {
		args map[string]any
		ctx  *tool.ToolContext
		want string
	}{
		{nil, nil, ""},
		{map[string]any{"worker_id": "mac"}, nil, "mac"},
		{map[string]any{"worker_id": "local"}, nil, ""},
		{map[string]any{"worker_id": "  "}, nil, ""},
		{nil, tool.NewToolContext(t.Context(), "", ""), ""},
	}
	ctx := tool.NewToolContext(t.Context(), "", "")
	ctx.Context = map[string]any{"worker_id": "from-context"}
	tests = append(tests, struct {
		args map[string]any
		ctx  *tool.ToolContext
		want string
	}{nil, ctx, "from-context"})

	for _, tc := range tests {
		got := ResolveWorkerID(tc.args, tc.ctx)
		if got != tc.want {
			t.Fatalf("ResolveWorkerID(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestIsRemoteExecution(t *testing.T) {
	if IsRemoteExecution(map[string]any{"worker_id": "w1"}, nil) != true {
		t.Fatal("expected remote")
	}
	if IsRemoteExecution(map[string]any{}, nil) {
		t.Fatal("expected local")
	}
}

func TestStripWorkerID(t *testing.T) {
	args := map[string]any{"worker_id": "w1", "cmd": "ls"}
	StripWorkerID(args)
	if _, ok := args["worker_id"]; ok {
		t.Fatal("worker_id should be stripped")
	}
	if args["cmd"] != "ls" {
		t.Fatal("other args preserved")
	}
}
