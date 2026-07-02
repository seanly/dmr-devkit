package tape

// CompactSummaryData holds the parsed content and schema version of a
// compact_summary entry.
type CompactSummaryData struct {
	SchemaVersion int
	Content       string
	SourceAnchor  string // anchor this summary was generated from
}

// ExtractCompactSummary parses a compact_summary payload. If schema_version is
// missing or zero, it defaults to CompactSummarySchemaVersion for backward
// compatibility with legacy entries.
func ExtractCompactSummary(payload map[string]any) (CompactSummaryData, bool) {
	content, ok := payload["content"].(string)
	if !ok {
		return CompactSummaryData{}, false
	}

	version := CompactSummarySchemaVersion
	if v, ok := payload["schema_version"].(int); ok && v > 0 {
		version = v
	} else if v, ok := payload["schema_version"].(float64); ok && v > 0 {
		version = int(v)
	} else if v, ok := payload["schema_version"].(int64); ok && v > 0 {
		version = int(v)
	}

	sourceAnchor, _ := payload["source_anchor"].(string)
	return CompactSummaryData{SchemaVersion: version, Content: content, SourceAnchor: sourceAnchor}, true
}
