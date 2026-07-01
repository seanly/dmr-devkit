package tape

import "testing"

func TestNewCompactSummaryEntry_DefaultsToV1(t *testing.T) {
	entry := NewCompactSummaryEntry("summary content")
	if entry.Kind != "compact_summary" {
		t.Fatalf("expected kind compact_summary, got %q", entry.Kind)
	}
	if got := entry.Payload["content"]; got != "summary content" {
		t.Errorf("content = %v, want %q", got, "summary content")
	}
	if got := entry.Payload["schema_version"]; got != CompactSummarySchemaVersion {
		t.Errorf("payload schema_version = %v, want %d", got, CompactSummarySchemaVersion)
	}
	if entry.Meta == nil {
		t.Fatal("expected meta to be set")
	}
	if got := entry.Meta["schema_version"]; got != CompactSummarySchemaVersion {
		t.Errorf("meta schema_version = %v, want %d", got, CompactSummarySchemaVersion)
	}
}

func TestNewCompactSummaryEntryWithVersion(t *testing.T) {
	entry := NewCompactSummaryEntryWithVersion("v2 summary", 2)
	if got := entry.Payload["schema_version"]; got != 2 {
		t.Errorf("payload schema_version = %v, want 2", got)
	}
	if got := entry.Meta["schema_version"]; got != 2 {
		t.Errorf("meta schema_version = %v, want 2", got)
	}
}
