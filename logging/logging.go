// Package logging provides a unified slog-based logger for DMR.
// Call Init early in main to set the global slog default and redirect
// the standard log package through slog.
package logging

import (
	"log"
	"log/slog"
	"os"
)

// Init configures the global slog logger and redirects the standard
// log package through it. verbosity: 0=warn+error only, 1=info,
// 2=debug, 3+=debug (trace-level messages use slog.Debug with attrs).
func Init(verbosity int) {
	level := slog.LevelWarn
	switch {
	case verbosity >= 2:
		level = slog.LevelDebug
	case verbosity >= 1:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Bridge standard log.Printf calls through slog so that existing
	// log.Printf("[WARN] ...") calls get slog formatting.
	log.SetFlags(0)
	log.SetOutput(&slogBridge{logger: logger})
}

// slogBridge implements io.Writer and routes log.Printf output to slog.
// It parses the [LEVEL] prefix if present to pick the right slog level.
type slogBridge struct {
	logger *slog.Logger
}

func (b *slogBridge) Write(p []byte) (int, error) {
	msg := string(p)
	// Trim trailing newline
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}

	level, body := parseLevel(msg)
	b.logger.Log(nil, level, body)
	return len(p), nil
}

// parseLevel extracts a [LEVEL] prefix from a log message.
func parseLevel(msg string) (slog.Level, string) {
	prefixes := []struct {
		tag   string
		level slog.Level
	}{
		{"[ERROR] ", slog.LevelError},
		{"[WARN] ", slog.LevelWarn},
		{"[INFO] ", slog.LevelInfo},
		{"[DEBUG] ", slog.LevelDebug},
		{"[TRACE] ", slog.LevelDebug}, // trace maps to debug
		{"[AUDIT] ", slog.LevelInfo},
	}
	for _, p := range prefixes {
		if len(msg) >= len(p.tag) && msg[:len(p.tag)] == p.tag {
			return p.level, msg[len(p.tag):]
		}
	}
	// No recognized prefix — default to Info
	return slog.LevelInfo, msg
}
