package toolresult

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// NormalizeEmpty replaces whitespace-only tool text with a short placeholder so the model always has tokens to attend.
func NormalizeEmpty(content, toolName string) string {
	if strings.TrimSpace(content) == "" {
		return fmt.Sprintf(EmptyResultPlaceholderFormat, toolName)
	}
	return content
}

// GeneratePreview returns a newline-aware prefix of content for persisted-output blocks.
func GeneratePreview(content string, maxRunes int) (preview string, hasMore bool) {
	if maxRunes <= 0 {
		maxRunes = DefaultPreviewRunes
	}
	total := utf8.RuneCountInString(content)
	if total <= maxRunes {
		return content, false
	}
	runes := []rune(content)
	trunc := string(runes[:maxRunes])
	lastNL := strings.LastIndex(trunc, "\n")
	cutPoint := maxRunes
	if lastNL > maxRunes/2 {
		cutPoint = lastNL
	}
	preview = string(runes[:cutPoint])
	return preview, len([]rune(preview)) < total
}

func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	kb := float64(n) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	mb := kb / 1024
	return fmt.Sprintf("%.1f MB", mb)
}
