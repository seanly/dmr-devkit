package frontmatter

import (
	"strings"
	"testing"
	"time"
)

func TestRenderDropsEmptyFields(t *testing.T) {
	m := &Meta{
		Type:        "table",
		Title:       "Orders",
		Description: "",  // empty -> dropped
		Tags:        nil, // nil -> dropped
		Status:      "",  // empty -> dropped
	}
	out, err := Render(m)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasPrefix(out, "type:") {
		t.Errorf("type not first:\n%s", out)
	}
	if strings.Contains(out, "description:") {
		t.Errorf("empty description should be dropped:\n%s", out)
	}
	if strings.Contains(out, "tags:") {
		t.Errorf("nil tags should be dropped:\n%s", out)
	}
	if strings.Contains(out, "status:") {
		t.Errorf("empty status should be dropped:\n%s", out)
	}
}

func TestRenderDocumentRoundTrip(t *testing.T) {
	body := "# Orders\n\nThis table holds customer orders.\n"
	m := &Meta{
		Type:        "table",
		Title:       "Orders",
		Description: "Customer orders table",
		Tags:        []string{"ecommerce", "sales"},
		Status:      "stable",
	}
	doc, err := RenderDocument(m, body)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	res, err := Extract(doc)
	if err != nil {
		t.Fatalf("Extract round-trip: %v\n---\n%s", err, doc)
	}
	if res.Meta.Type != "table" {
		t.Errorf("type = %q", res.Meta.Type)
	}
	if res.Meta.Title != "Orders" {
		t.Errorf("title = %q", res.Meta.Title)
	}
	if res.Meta.Description != "Customer orders table" {
		t.Errorf("description = %q", res.Meta.Description)
	}
	if len(res.Meta.Tags) != 2 || res.Meta.Tags[0] != "ecommerce" {
		t.Errorf("tags = %v", res.Meta.Tags)
	}
	if res.Meta.Status != "stable" {
		t.Errorf("status = %q", res.Meta.Status)
	}
	if got := strings.TrimSpace(res.Body); got != strings.TrimSpace(body) {
		t.Errorf("body round-trip mismatch:\nwant %q\ngot  %q", body, got)
	}
}

func TestRenderPreservesVerifiedAndSources(t *testing.T) {
	m := &Meta{
		Type:      "reference",
		Title:     "API Doc",
		Generated: &ActorEvent{By: "agent:foo", At: time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)},
		Verified: VerifiedList{
			{By: "machine:scraper", At: time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)},
		},
		Sources: []Source{
			{ID: "src1", Resource: "https://example.com", Title: "Example"},
		},
	}
	doc, err := RenderDocument(m, "body text")
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	res, err := Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v\n%s", err, doc)
	}
	if res.Meta.Generated == nil || res.Meta.Generated.By != "agent:foo" {
		t.Errorf("generated not round-tripped: %+v", res.Meta.Generated)
	}
	if len(res.Meta.Verified) != 1 || res.Meta.Verified[0].By != "machine:scraper" {
		t.Errorf("verified not round-tripped: %+v", res.Meta.Verified)
	}
	if len(res.Meta.Sources) != 1 || res.Meta.Sources[0].Resource != "https://example.com" {
		t.Errorf("sources not round-tripped: %+v", res.Meta.Sources)
	}
}

func TestRenderRejectsMissingType(t *testing.T) {
	m := &Meta{Title: "no type"}
	if _, err := Render(m); err == nil {
		t.Fatal("expected error for missing type")
	}
}
