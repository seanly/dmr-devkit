package tools

import (
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/okf/bundle"
	"github.com/seanly/dmr-devkit/okf/service"
)

func findTool(ts []*tool.Tool, name string) *tool.Tool {
	for _, t := range ts {
		if t.Spec.Name == name {
			return t
		}
	}
	return nil
}

func newSvc(t *testing.T) *service.Service {
	t.Helper()
	loader := bundle.NewLoader()
	b, err := loader.Load("../service/testdata/ecommerce-kb")
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	return service.New(b)
}

func toolNames(ts []*tool.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Spec.Name
	}
	return out
}

func TestReadTools(t *testing.T) {
	svc := newSvc(t)
	tools := ReadTools(svc)

	// 8 consumer + 5 dev + present_investigation = 14. No VCS attached to the
	// plain example bundle, so GitReadTools returns nil and adds nothing.
	wantCount := 14
	if len(tools) != wantCount {
		t.Fatalf("ReadTools: got %d tools, want %d (%v)", len(tools), wantCount, toolNames(tools))
	}

	want := map[string]bool{
		"list_concepts": true, "search_concepts": true, "get_concept": true,
		"get_index": true, "get_neighbors": true, "get_backlinks": true,
		"check_stale": true, "get_trusted": true,
		"validate_bundle": true, "bundle_stats": true, "list_types": true,
		"get_graph": true, "get_graph_summary": true,
		"present_investigation": true,
	}
	for _, n := range toolNames(tools) {
		if !want[n] {
			t.Errorf("unexpected tool in ReadTools: %s", n)
		}
	}

	// ReadTools must contain no mutation tools.
	for _, bad := range []string{"create_concept", "delete_concept", "commit", "git_revert"} {
		for _, n := range toolNames(tools) {
			if n == bad {
				t.Errorf("ReadTools must not include mutation tool %s", bad)
			}
		}
	}
}

func TestWriteTools(t *testing.T) {
	svc := newSvc(t)
	tools := WriteTools(svc)

	// 5 producer tools. No VCS attached → GitWriteTools returns nil.
	wantCount := 5
	if len(tools) != wantCount {
		t.Fatalf("WriteTools: got %d tools, want %d (%v)", len(tools), wantCount, toolNames(tools))
	}

	want := map[string]bool{
		"create_concept": true, "update_concept": true, "set_frontmatter": true,
		"append_section": true, "delete_concept": true,
	}
	for _, n := range toolNames(tools) {
		if !want[n] {
			t.Errorf("unexpected tool in WriteTools: %s", n)
		}
	}
}

func TestConsumerAndDevCounts(t *testing.T) {
	svc := newSvc(t)
	if got := len(ConsumerTools(svc)); got != 8 {
		t.Errorf("ConsumerTools: got %d, want 8", got)
	}
	if got := len(DevTools(svc)); got != 5 {
		t.Errorf("DevTools: got %d, want 5", got)
	}
	if got := len(ProducerTools(svc)); got != 5 {
		t.Errorf("ProducerTools: got %d, want 5", got)
	}
}

// TestRoutingByBundleParam verifies the per-call "bundle" selector flows from
// tool args through the Resolver, and that an empty selector hits the default.
func TestRoutingByBundleParam(t *testing.T) {
	svc := newSvc(t)
	var requested string
	r := Resolver(func(b string) (*service.Service, error) {
		requested = b
		return svc, nil
	})
	lc := findTool(ReadToolsWith(r), "list_concepts")
	if lc == nil {
		t.Fatal("list_concepts not in ReadToolsWith")
	}
	if _, err := lc.Handler(nil, map[string]any{"bundle": "repo-x"}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if requested != "repo-x" {
		t.Errorf("resolver got %q, want repo-x", requested)
	}
	// Empty/absent bundle → default (resolver receives "").
	if _, err := lc.Handler(nil, map[string]any{}); err != nil {
		t.Fatalf("default handler: %v", err)
	}
	if requested != "" {
		t.Errorf("default resolver got %q, want empty", requested)
	}
}

// TestResolverUnknownBundleErrors verifies a resolver error surfaces from the
// handler (the agent gets a clear "unknown bundle" failure rather than a nil
// deref).
func TestResolverUnknownBundleErrors(t *testing.T) {
	r := Resolver(func(b string) (*service.Service, error) {
		if b == "" {
			return nil, fmt.Errorf("no bundles mounted")
		}
		return nil, fmt.Errorf("unknown bundle %q; call list_bundles", b)
	})
	lc := findTool(ReadToolsWith(r), "list_concepts")
	if _, err := lc.Handler(nil, map[string]any{"bundle": "nope"}); err == nil {
		t.Error("expected error for unknown bundle, got nil")
	}
	if _, err := lc.Handler(nil, map[string]any{}); err == nil {
		t.Error("expected error for no bundles mounted, got nil")
	}
}

// TestEveryReadToolHasBundleParam ensures the bundle selector is exposed on
// every svc-backed read tool (present_investigation is svc-free and exempt).
func TestEveryReadToolHasBundleParam(t *testing.T) {
	svc := newSvc(t)
	for _, tt := range ReadToolsWith(SingleResolver(svc)) {
		if tt.Spec.Name == "present_investigation" {
			continue
		}
		props, _ := tt.Spec.Parameters["properties"].(map[string]any)
		if _, ok := props["bundle"]; !ok {
			t.Errorf("tool %q missing bundle parameter", tt.Spec.Name)
		}
	}
}
