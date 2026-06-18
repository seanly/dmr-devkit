package a2ui

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestFixJSONBalanceBracketsSmart(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, got string)
	}{
		{
			name:  "balanced unchanged",
			input: `{"a":1}`,
			check: func(t *testing.T, got string) {
				if got != `{"a":1}` {
					t.Errorf("got %q", got)
				}
			},
		},
		{
			name:  "missing closing brace",
			input: `{"a":1`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
		{
			name:  "missing closing bracket",
			input: `[1,2,3`,
			check: func(t *testing.T, got string) {
				var a []int
				if err := json.Unmarshal([]byte(got), &a); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
		{
			name:  "braces inside strings not counted",
			input: `{"key": "{not a brace}"}`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
		{
			name:  "simple truncated",
			input: `{"a":1`,
			check: func(t *testing.T, got string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("unmarshal failed: %v", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test balanceBracketsSmart directly
			got := balanceBracketsSmart(tt.input)
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestFixJSONStripMarkdownFences(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\n{\"a\":1}\n```", `{"a":1}`},
		{"{\"a\":1}", `{"a":1}`},
		{"```json{\"a\":1}```", `{"a":1}`},
	}
	for _, tt := range tests {
		got := stripMarkdownFences(tt.input)
		if got != tt.want {
			t.Errorf("stripMarkdownFences(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFixJSONFixSingleQuotes(t *testing.T) {
	// Basic case
	got := fixSingleQuotes(`{'a': 'b'}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil || m["a"] != "b" {
		t.Errorf("got %q, err %v", got, err)
	}
	// Inside double quotes preserved
	got2 := fixSingleQuotes(`{"a": "it's"}`)
	if err := json.Unmarshal([]byte(got2), &m); err != nil || m["a"] != "it's" {
		t.Errorf("got %q, err %v", got2, err)
	}
	// Mixed
	got3 := fixSingleQuotes(`{'a': "don't"}`)
	if err := json.Unmarshal([]byte(got3), &m); err != nil || m["a"] != "don't" {
		t.Errorf("got %q, err %v", got3, err)
	}
}

func TestFixJSONFixUnquotedKeys(t *testing.T) {
	got := fixUnquotedKeys(`{key: "value"}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil || m["key"] != "value" {
		t.Errorf("got %q, err %v", got, err)
	}
	got2 := fixUnquotedKeys(`{a:1, b: "2"}`)
	if err := json.Unmarshal([]byte(got2), &m); err != nil || fmt.Sprint(m["a"]) != "1" || m["b"] != "2" {
		t.Errorf("got %q, err %v", got2, err)
	}
	// Inside string should not be changed
	got3 := fixUnquotedKeys(`{"key": "some: value"}`)
	if err := json.Unmarshal([]byte(got3), &m); err != nil {
		t.Errorf("got %q, err %v", got3, err)
	}
}

func TestFixJSONFixTrailingCommas(t *testing.T) {
	got := fixTrailingCommas(`{"a":1,}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Errorf("got %q, err %v", got, err)
	}
	got2 := fixTrailingCommas(`[1,2,3,]`)
	var a []int
	if err := json.Unmarshal([]byte(got2), &a); err != nil {
		t.Errorf("got %q, err %v", got2, err)
	}
	// Inside string not changed
	got3 := fixTrailingCommas(`{"a": "text, "}`)
	if err := json.Unmarshal([]byte(got3), &m); err != nil || m["a"] != "text, " {
		t.Errorf("got %q, err %v", got3, err)
	}
}
