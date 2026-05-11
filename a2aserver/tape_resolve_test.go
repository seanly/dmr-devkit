package a2aserver

import (
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestSanitizeTapeSegment(t *testing.T) {
	t.Parallel()
	if g := SanitizeTapeSegment("abc-def_12"); g != "abc-def_12" {
		t.Fatalf("got %q", g)
	}
	g := SanitizeTapeSegment("../../x")
	if strings.Contains(g, "/") || strings.Contains(g, `\`) {
		t.Fatalf("path seps in %q", g)
	}
	if SanitizeTapeSegment("...") != "tape" {
		t.Fatal()
	}
}

func TestAutoTapeName(t *testing.T) {
	t.Parallel()
	id := a2a.TaskID("550e8400-e29b-41d4-a716-446655440000")
	if g := AutoTapeName("pfx", id); g != "pfx_550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("got %q", g)
	}
}
