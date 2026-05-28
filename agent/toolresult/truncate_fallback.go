package toolresult

import (
	"fmt"
	"unicode/utf8"
)

// TruncateWithHint keeps head and tail when persist is impossible (no workspace).
func TruncateWithHint(content string, maxRunes int) string {
	if maxRunes <= 0 || content == "" {
		return content
	}
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	half := maxRunes / 2
	head := string(runes[:half])
	tail := string(runes[len(runes)-(maxRunes-half):])
	return fmt.Sprintf(
		"%s\n\n... [omitted %d runes in the middle] ...\n"+
			"Tool output exceeded the configured limit; use pagination or narrower queries.\n\n%s",
		head, utf8.RuneCountInString(content)-maxRunes, tail,
	)
}
