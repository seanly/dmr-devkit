package tape

import "testing"

func TestExtractCompactSummary_MissingVersionDefaultsToV1(t *testing.T) {
	data, ok := ExtractCompactSummary(map[string]any{"content": "legacy summary"})
	if !ok {
		t.Fatal("expected ok for legacy payload")
	}
	if data.Content != "legacy summary" {
		t.Errorf("content = %q, want %q", data.Content, "legacy summary")
	}
	if data.SchemaVersion != CompactSummarySchemaVersion {
		t.Errorf("schema_version = %d, want %d", data.SchemaVersion, CompactSummarySchemaVersion)
	}
}

func TestExtractCompactSummary_IntVersion(t *testing.T) {
	data, ok := ExtractCompactSummary(map[string]any{"content": "v2 summary", "schema_version": 2})
	if !ok {
		t.Fatal("expected ok")
	}
	if data.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", data.SchemaVersion)
	}
}

func TestExtractCompactSummary_Float64Version(t *testing.T) {
	// JSON unmarshaling produces float64 for numbers.
	data, ok := ExtractCompactSummary(map[string]any{"content": "v3 summary", "schema_version": float64(3)})
	if !ok {
		t.Fatal("expected ok")
	}
	if data.SchemaVersion != 3 {
		t.Errorf("schema_version = %d, want 3", data.SchemaVersion)
	}
}

func TestExtractCompactSummary_ZeroVersionFallsBackToV1(t *testing.T) {
	data, ok := ExtractCompactSummary(map[string]any{"content": "zero version", "schema_version": 0})
	if !ok {
		t.Fatal("expected ok")
	}
	if data.SchemaVersion != CompactSummarySchemaVersion {
		t.Errorf("schema_version = %d, want %d", data.SchemaVersion, CompactSummarySchemaVersion)
	}
}

func TestExtractCompactSummary_MissingContent(t *testing.T) {
	_, ok := ExtractCompactSummary(map[string]any{"schema_version": 1})
	if ok {
		t.Error("expected not ok when content is missing")
	}
}
