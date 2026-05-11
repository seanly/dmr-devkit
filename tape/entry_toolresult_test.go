package tape

import "testing"

func TestExtractToolResults_plainString(t *testing.T) {
	payload := map[string]any{"results": []any{"hello output"}}
	got, ok := ExtractToolResults(payload)
	if !ok || len(got) != 1 || got[0].Content != "hello output" {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestExtractToolResults_errorMap(t *testing.T) {
	payload := map[string]any{"results": []any{map[string]any{"kind": "tool", "message": "boom"}}}
	got, ok := ExtractToolResults(payload)
	if !ok || len(got) != 1 || got[0].Content != "tool: boom" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractToolResults_contentMap(t *testing.T) {
	payload := map[string]any{"results": []any{map[string]any{"content": "body"}}}
	got, ok := ExtractToolResults(payload)
	if !ok || len(got) != 1 || got[0].Content != "body" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractToolResults_number(t *testing.T) {
	payload := map[string]any{"results": []any{float64(42)}}
	got, ok := ExtractToolResults(payload)
	if !ok || len(got) != 1 || got[0].Content != "42" {
		t.Fatalf("got %#v", got)
	}
}
