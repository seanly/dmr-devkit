package memory

import (
	"strings"
	"testing"
)

func TestHandleMemoryPut_NoBackend(t *testing.T) {
	p := &Service{}
	out, err := p.handleMemoryPut(nil, map[string]any{
		"slug":    "notes/x",
		"content": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryGet_NoBackend(t *testing.T) {
	p := &Service{}
	out, err := p.handleMemoryGet(nil, map[string]any{"slug": "notes/x"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryPut_InvalidPageType(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	out, err := p.handleMemoryPut(nil, map[string]any{
		"slug":    "notes/bad-type",
		"content": "body",
		"type":    "not-a-real-type",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryPut_FrontmatterNotObject(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	out, err := p.handleMemoryPut(nil, map[string]any{
		"slug":        "notes/fm",
		"content":     "body",
		"frontmatter": "not-an-object",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryPut_TagsNonString(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	out, err := p.handleMemoryPut(nil, map[string]any{
		"slug":    "notes/tags",
		"content": "body",
		"tags":    []any{"a", 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestMemoryPutToolSpec_Registered(t *testing.T) {
	p := &Service{}
	tool := p.memoryPutTool()
	if tool == nil || tool.Spec.Name != "memoryPut" {
		t.Fatal("memoryPut tool missing")
	}
	if tool.Handler == nil {
		t.Fatal("handler nil")
	}
}

func TestHandleMemorySearch_NoBackend(t *testing.T) {
	p := &Service{}
	out, err := p.handleMemorySearch(nil, map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestValidatePageType(t *testing.T) {
	if err := ValidatePageType(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePageType("note"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePageType("bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateSlugPrefix(t *testing.T) {
	if err := ValidateSlugPrefix(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSlugPrefix("people/"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSlugPrefix("../x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleMemoryGet_InvalidSlug(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	out, err := p.handleMemoryGet(nil, map[string]any{"slug": "../escape"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected validation error, got %#v", out)
	}
}

func TestHandleMemoryList_InvalidTypeFilter(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	out, err := p.handleMemoryList(nil, map[string]any{"type": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryTags_Add_NonStringTag(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	_, _ = p.handleMemoryPut(nil, map[string]any{"slug": "notes/tag-page", "content": "c"})
	out, err := p.handleMemoryTags(nil, map[string]any{
		"slug":   "notes/tag-page",
		"action": "add",
		"tags":   []any{"ok", 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryTimeline_InvalidDate(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	_, _ = p.handleMemoryPut(nil, map[string]any{"slug": "notes/tl", "content": "c"})
	out, err := p.handleMemoryTimeline(nil, map[string]any{
		"slug":    "notes/tl",
		"action":  "add",
		"date":    "not-a-date",
		"summary": "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryStatus_NoBackend(t *testing.T) {
	p := &Service{}
	out, err := p.handleMemoryStatus(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryStatus_WithBackend(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	_, _ = p.handleMemoryPut(nil, map[string]any{"slug": "notes/st", "content": "x"})
	out, err := p.handleMemoryStatus(nil, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != true {
		t.Fatalf("expected success, got %#v", out)
	}
	if m["backend"] != "sqlite" {
		t.Errorf("backend = %v, want sqlite", m["backend"])
	}
	if mustInt(t, m["page_count"]) < 1 {
		t.Error("page_count should be at least 1 after put")
	}
}

func TestHandleMemoryRevisions_NoBackend(t *testing.T) {
	p := &Service{}
	out, err := p.handleMemoryRevisions(nil, map[string]any{
		"slug":   "notes/x",
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected success=false, got %#v", out)
	}
}

func TestHandleMemoryRevisions_List_NoRevisionsAfterSinglePut(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	_, _ = p.handleMemoryPut(nil, map[string]any{"slug": "notes/once", "content": "v"})
	out, err := p.handleMemoryRevisions(nil, map[string]any{
		"slug":   "notes/once",
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := out.(string)
	if !ok || !strings.Contains(s, "No revisions") {
		t.Fatalf("expected no-revisions message, got %#v", out)
	}
}

func TestHandleMemoryRevisions_ListAndRevert(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	_, _ = p.handleMemoryPut(nil, map[string]any{"slug": "notes/rv", "content": "first"})
	_, _ = p.handleMemoryPut(nil, map[string]any{"slug": "notes/rv", "content": "second"})

	out, err := p.handleMemoryRevisions(nil, map[string]any{
		"slug":   "notes/rv",
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != true {
		t.Fatalf("list: want success, got %#v", out)
	}
	revs, ok := m["revisions"].([]PageRevision)
	if !ok || len(revs) != 1 {
		t.Fatalf("revisions: %#v", m["revisions"])
	}
	rid := revs[0].ID
	if rid <= 0 {
		t.Fatalf("revision id: %d", rid)
	}

	out, err = p.handleMemoryRevisions(nil, map[string]any{
		"slug":        "notes/rv",
		"action":      "revert",
		"revision_id": float64(rid), // simulates JSON number
	})
	if err != nil {
		t.Fatal(err)
	}
	rm, ok := out.(map[string]any)
	if !ok || rm["success"] != true {
		t.Fatalf("revert: %#v", out)
	}
	if int(mustInt(t, rm["reverted_to_revision_id"])) != rid {
		t.Errorf("reverted_to_revision_id = %v, want %d", rm["reverted_to_revision_id"], rid)
	}

	g, err := p.handleMemoryGet(nil, map[string]any{"slug": "notes/rv"})
	if err != nil {
		t.Fatal(err)
	}
	pgm, ok := g.(map[string]any)
	if !ok {
		t.Fatalf("get return type %T", g)
	}
	if pgm["content"] != "first" {
		t.Errorf("content after revert = %q, want first", pgm["content"])
	}
}

func TestHandleMemoryRevisions_Revert_MissingRevisionID(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	_, _ = p.handleMemoryPut(nil, map[string]any{"slug": "notes/rev-need-id", "content": "a"})
	out, err := p.handleMemoryRevisions(nil, map[string]any{
		"slug":   "notes/rev-need-id",
		"action": "revert",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected error result, got %#v", out)
	}
}

func TestHandleMemoryRevisions_UnknownAction(t *testing.T) {
	p := &Service{backend: setupBackend(t)}
	out, err := p.handleMemoryRevisions(nil, map[string]any{
		"slug":   "notes/rv",
		"action": "nope",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["success"] != false {
		t.Fatalf("expected failure, got %#v", out)
	}
}

func TestListTools_IncludesStatusAndRevisions(t *testing.T) {
	p := &Service{}
	tools := p.Tools()
	names := make(map[string]struct{}, len(tools))
	for _, tl := range tools {
		names[tl.Spec.Name] = struct{}{}
	}
	if _, ok := names["memoryStatus"]; !ok {
		t.Error("ListTools: missing memoryStatus")
	}
	if _, ok := names["memoryRevisions"]; !ok {
		t.Error("ListTools: missing memoryRevisions")
	}
}

func TestMemoryStatusAndRevisionsToolSpec(t *testing.T) {
	p := &Service{}
	s := p.memoryStatusTool()
	if s == nil || s.Spec.Name != "memoryStatus" || s.Handler == nil {
		t.Fatalf("memoryStatus tool: %#v", s)
	}
	r := p.memoryRevisionsTool()
	if r == nil || r.Spec.Name != "memoryRevisions" || r.Handler == nil {
		t.Fatalf("memoryRevisions tool: %#v", r)
	}
}

func mustInt(t *testing.T, v any) int {
	t.Helper()
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		t.Fatalf("not a number: %T %v", v, v)
	}
	return 0
}
