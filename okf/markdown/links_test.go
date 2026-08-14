package markdown

import (
	"testing"
)

func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "single link",
			input: "[text](path/to/file.md)",
			want:  1,
		},
		{
			name:  "skip http",
			input: "[a](http://example.com) [b](local.md)",
			want:  1,
		},
		{
			name:  "skip anchor",
			input: "[a](#section) [b](file.md)",
			want:  1,
		},
		{
			name:  "no links",
			input: "plain text",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLinks(tt.input)
			if len(got) != tt.want {
				t.Errorf("ExtractLinks() = %d links, want %d", len(got), tt.want)
			}
		})
	}
}
