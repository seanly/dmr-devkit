package logging

import (
	"io"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input     string
		wantLevel slog.Level
		wantBody  string
	}{
		{"[ERROR] something broke", slog.LevelError, "something broke"},
		{"[WARN] be careful", slog.LevelWarn, "be careful"},
		{"[INFO] fyi", slog.LevelInfo, "fyi"},
		{"[DEBUG] details", slog.LevelDebug, "details"},
		{"[TRACE] very detailed", slog.LevelDebug, "very detailed"},
		{"[AUDIT] user did thing", slog.LevelInfo, "user did thing"},
		{"no prefix here", slog.LevelInfo, "no prefix here"},
		{"", slog.LevelInfo, ""},
		{"[UNKNOWN] tag", slog.LevelInfo, "[UNKNOWN] tag"},
	}

	for _, tt := range tests {
		level, body := parseLevel(tt.input)
		if level != tt.wantLevel {
			t.Errorf("parseLevel(%q) level = %v, want %v", tt.input, level, tt.wantLevel)
		}
		if body != tt.wantBody {
			t.Errorf("parseLevel(%q) body = %q, want %q", tt.input, body, tt.wantBody)
		}
	}
}

func TestSlogBridge_Write(t *testing.T) {
	// Verify the bridge doesn't panic and returns correct byte count.
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	b := &slogBridge{logger: logger}

	msg := []byte("[INFO] hello world\n")
	n, err := b.Write(msg)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(msg) {
		t.Errorf("Write returned %d, want %d", n, len(msg))
	}
}

func TestInit_DoesNotPanic(t *testing.T) {
	// Init should configure slog without panicking at any verbosity.
	for _, v := range []int{0, 1, 2, 3} {
		Init(v)
	}
}
