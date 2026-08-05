package client

import (
	"strings"
	"testing"
)

func TestEstimator_ImageDataURIDoesNotScaleWithBase64(t *testing.T) {
	e := NewEstimator()

	small := e.Estimate([]map[string]any{{
		"role": "user",
		"parts": []any{
			map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "data:image/png;base64,abc"},
			},
		},
	}})
	large := e.Estimate([]map[string]any{{
		"role": "user",
		"parts": []any{
			map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "data:image/png;base64," + strings.Repeat("A", 2_000_000)},
			},
		},
	}})

	if small <= 0 || large <= 0 {
		t.Fatalf("expected non-zero image estimates, small=%d large=%d", small, large)
	}
	if large > small*2 {
		t.Fatalf("image estimate scaled with base64 length: small=%d large=%d", small, large)
	}
	if large > 20_000 {
		t.Fatalf("image estimate too large for context budgeting: %d", large)
	}
}

func TestTruncateMessagesToLimit_PreservesHistoryWithLargeImage(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "you are helpful"},
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "user", "content": "hi?"},
		{"role": "assistant", "content": "online"},
		{
			"role": "user",
			"content": "image",
			"parts": []any{
				map[string]any{"type": "text", "text": "image"},
				map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": "data:image/jpeg;base64," + strings.Repeat("B", 3_000_000)},
				},
			},
		},
	}

	out := truncateMessagesToLimit(msgs, 200_000)
	if len(out) != len(msgs) {
		t.Fatalf("expected history preserved, before=%d after=%d", len(msgs), len(out))
	}
}
