package tape

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildChatExportMessages_mergesToolCalls(t *testing.T) {
	entries := []TapeEntry{
		NewMessageEntry(map[string]any{"role": "user", "content": "hi"}),
		NewToolCallEntry([]map[string]any{{
			"id": "1", "type": "function",
			"function": map[string]any{"name": "grep", "arguments": `{"q":"x"}`},
		}}),
		NewToolResultEntry([]any{map[string]any{"content": "found"}}),
		NewMessageEntry(map[string]any{"role": "assistant", "content": "done"}),
	}
	msgs := BuildChatExportMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hi" {
		t.Fatalf("first msg: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "done" {
		t.Fatalf("second msg: %+v", msgs[1])
	}
	if len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(msgs[1].ToolCalls))
	}
	tc := msgs[1].ToolCalls[0]
	if tc.Name != "grep" || tc.Arguments != `{"q":"x"}` || tc.Result != "found" {
		t.Fatalf("tool: %+v", tc)
	}
}

func TestBuildChatExportMessages_systemAndCompact(t *testing.T) {
	entries := []TapeEntry{
		NewSystemEntry("sys"),
		NewCompactSummaryEntry("sum"),
		NewMessageEntry(map[string]any{"role": "user", "content": "u"}),
	}
	msgs := BuildChatExportMessages(entries)
	if len(msgs) != 3 {
		t.Fatalf("want 3 msgs, got %d %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "system" || msgs[0].Content != "sys" {
		t.Fatalf("msg0 %+v", msgs[0])
	}
	if msgs[1].Role != "system" || msgs[1].Content != "[Context Summary]\nsum" {
		t.Fatalf("msg1 %+v", msgs[1])
	}
}

func TestExportTape_JSON_transcript(t *testing.T) {
	entries := []TapeEntry{
		{ID: 1, Kind: "message", Payload: map[string]any{"role": "user", "content": "x"}, Date: "2026-01-01T00:00:00Z"},
	}
	meta := ExportMeta{TapeName: "t1", SliceDescription: "full", GeneratedAt: "2026-01-02T00:00:00Z"}
	out, err := ExportTape(meta, entries, ExportOptions{Format: ExportFormatJSON, Mode: ExportModeTranscript})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["version"]) != "1" {
		t.Fatalf("version: %s", doc["version"])
	}
	var entriesArr []TapeEntry
	if err := json.Unmarshal(doc["entries"], &entriesArr); err != nil {
		t.Fatal(err)
	}
	if len(entriesArr) != 1 || entriesArr[0].Kind != "message" {
		t.Fatalf("entries: %+v", entriesArr)
	}
}

func TestExportTape_JSON_chat(t *testing.T) {
	entries := []TapeEntry{
		NewMessageEntry(map[string]any{"role": "user", "content": "hi"}),
	}
	meta := ExportMeta{TapeName: "t1", SliceDescription: "full"}
	out, err := ExportTape(meta, entries, ExportOptions{Format: ExportFormatJSON, Mode: ExportModeChat})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Entries []ChatExportMessage `json:"entries"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 1 || doc.Entries[0].Content != "hi" {
		t.Fatalf("%+v", doc.Entries)
	}
}

func TestExportTape_markdown_containsTapeName(t *testing.T) {
	entries := []TapeEntry{NewMessageEntry(map[string]any{"role": "user", "content": "body"})}
	meta := ExportMeta{TapeName: "cli:main", SliceDescription: "after anchor a"}
	out, err := ExportTape(meta, entries, ExportOptions{Format: ExportFormatMarkdown, Mode: ExportModeTranscript})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "cli:main") || !strings.Contains(s, "after anchor a") || !strings.Contains(s, "body") {
		t.Fatalf("%s", s)
	}
}

func TestExportTape_HTML_escapesPayload(t *testing.T) {
	entries := []TapeEntry{
		{ID: 1, Kind: "message", Payload: map[string]any{"role": "user", "content": "<script>x</script>"}, Date: "d"},
	}
	meta := ExportMeta{TapeName: "t", SliceDescription: "s"}
	out, err := ExportTape(meta, entries, ExportOptions{Format: ExportFormatHTML, Mode: ExportModeTranscript})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Inline expand/collapse script is expected; user body must stay escaped in <pre>.
	if !strings.Contains(s, "&lt;script&gt;x&lt;/script&gt;") {
		t.Fatalf("expected escaped script in message body: %s", s)
	}
}

func TestExportTape_HTML_foldableDetails(t *testing.T) {
	entries := []TapeEntry{
		{ID: 1, Kind: "message", Payload: map[string]any{"role": "user", "content": "hi"}, Date: "d"},
	}
	out, err := ExportTape(ExportMeta{TapeName: "t", SliceDescription: "s"}, entries, ExportOptions{Format: ExportFormatHTML, Mode: ExportModeTranscript})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `<details class="foldable`) || !strings.Contains(s, "<summary") {
		t.Fatalf("expected details/summary fold: %s", s)
	}
	if !strings.Contains(s, "expand-all") || !strings.Contains(s, "collapse-all") {
		t.Fatal("expected expand/collapse all controls")
	}
}

func TestExportTape_HTML_messageIsPlainTextNotRendered(t *testing.T) {
	entries := []TapeEntry{
		{ID: 1, Kind: "message", Payload: map[string]any{"role": "user", "content": "## Title\n\nHello **world**."}, Date: "d"},
	}
	meta := ExportMeta{TapeName: "t", SliceDescription: "s"}
	out, err := ExportTape(meta, entries, ExportOptions{Format: ExportFormatHTML, Mode: ExportModeTranscript})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "## Title") || !strings.Contains(s, "**world**") {
		t.Fatalf("expected raw markdown preserved in body: %s", s)
	}
}

func TestExportTape_unknownFormat(t *testing.T) {
	_, err := ExportTape(ExportMeta{}, nil, ExportOptions{Format: ExportFormat("nope"), Mode: ExportModeTranscript})
	if err == nil {
		t.Fatal("expected error")
	}
}
