package provider

import "testing"

func TestStripImagePartsFromMessages(t *testing.T) {
	t.Parallel()
	msgs := []map[string]any{
		{"role": "user", "content": "hello"},
		{
			"role": "user",
			"content": "what is this?",
			"parts": []any{
				map[string]any{"type": "text", "text": "what is this?"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			},
		},
	}
	out := StripImagePartsFromMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	parts, ok := out[1]["parts"].([]any)
	if !ok {
		t.Fatalf("parts missing: %#v", out[1])
	}
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	pm, _ := parts[0].(map[string]any)
	if pm["type"] != "text" {
		t.Fatalf("part type = %v, want text", pm["type"])
	}
}

func TestStripImagePartsFromMessages_imageOnly(t *testing.T) {
	t.Parallel()
	msgs := []map[string]any{
		{
			"role": "user",
			"parts": []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			},
		},
	}
	out := StripImagePartsFromMessages(msgs)
	if out[0]["content"] != imageOmittedPlaceholder {
		t.Fatalf("content = %q, want placeholder", out[0]["content"])
	}
	if _, ok := out[0]["parts"]; ok {
		t.Fatal("parts should be removed")
	}
}

func TestStripImageContentParts(t *testing.T) {
	t.Parallel()
	in := []ContentPart{
		TextPart{Text: "hi"},
		ImagePart{URL: "data:image/png;base64,x"},
	}
	out := StripImageContentParts(in)
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if _, ok := out[0].(TextPart); !ok {
		t.Fatalf("want TextPart, got %T", out[0])
	}
}
