package agent

import "testing"

func TestIntFromUsageMap(t *testing.T) {
	tests := []struct {
		u    map[string]any
		key  string
		want int
		ok   bool
	}{
		{map[string]any{"prompt_tokens": 100}, "prompt_tokens", 100, true},
		{map[string]any{"prompt_tokens": int32(50)}, "prompt_tokens", 50, true},
		{map[string]any{"prompt_tokens": int64(999)}, "prompt_tokens", 999, true},
		{map[string]any{"prompt_tokens": float64(42)}, "prompt_tokens", 42, true},
		{map[string]any{}, "prompt_tokens", 0, false},
		{map[string]any{"prompt_tokens": "x"}, "prompt_tokens", 0, false},
		{map[string]any{"completion_tokens": 200}, "completion_tokens", 200, true},
		{map[string]any{"completion_tokens": float64(55)}, "completion_tokens", 55, true},
		{map[string]any{}, "completion_tokens", 0, false},
	}
	for _, tc := range tests {
		got, ok := intFromUsageMap(tc.u, tc.key)
		if ok != tc.ok || got != tc.want {
			t.Errorf("intFromUsageMap(%v, %q): got (%d,%v) want (%d,%v)", tc.u, tc.key, got, ok, tc.want, tc.ok)
		}
	}
}
