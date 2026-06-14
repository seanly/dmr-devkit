package bridge

import "testing"

func TestResolveWSURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://dmr.example.com", "wss://dmr.example.com/api/plugin/localbridge/connect"},
		{"http://localhost:8080", "ws://localhost:8080/api/plugin/localbridge/connect"},
		{"wss://dmr.example.com/custom/connect", "wss://dmr.example.com/custom/connect"},
	}
	for _, tt := range tests {
		got, err := ResolveWSURL(tt.in)
		if err != nil {
			t.Fatalf("ResolveWSURL(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ResolveWSURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveWSURLEmpty(t *testing.T) {
	if _, err := ResolveWSURL(""); err == nil {
		t.Fatal("expected error")
	}
}
