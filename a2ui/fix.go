package a2ui

import (
	"fmt"
	"regexp"
	"strings"
)

// FixJSON auto-fixes common JSON issues produced by LLMs.
// It handles: unquoted keys, trailing commas, single quotes, markdown fences.
func FixJSON(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("empty JSON")
	}

	// Strip markdown code fences.
	s = stripMarkdownFences(s)

	// Fix single quotes to double quotes (only outside strings).
	s = fixSingleQuotes(s)

	// Fix unquoted keys.
	s = fixUnquotedKeys(s)

	// Fix trailing commas before } and ].
	s = fixTrailingCommas(s)

	// Balance braces/brackets only if the JSON is structurally truncated.
	s = balanceBracketsSmart(s)

	return s, nil
}

var markdownFenceRe = regexp.MustCompile("(?s)^```(json)?\\s*\\n?|\\n?```\\s*$")

func stripMarkdownFences(s string) string {
	return markdownFenceRe.ReplaceAllString(s, "")
}

func fixSingleQuotes(s string) string {
	var out strings.Builder
	inString := false
	escape := false
	for _, r := range s {
		if escape {
			out.WriteRune(r)
			escape = false
			continue
		}
		if r == '\\' {
			out.WriteRune(r)
			escape = true
			continue
		}
		if r == '"' {
			inString = !inString
			out.WriteRune(r)
			continue
		}
		if r == '\'' && !inString {
			out.WriteRune('"')
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// fixUnquotedKeys adds double quotes around unquoted object keys.
// It skips matches that are already inside strings by looking for an odd
// number of unescaped quotes before the match.
var unquotedKeyRe = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)

func fixUnquotedKeys(s string) string {
	return unquotedKeyRe.ReplaceAllStringFunc(s, func(m string) string {
		loc := unquotedKeyRe.FindStringIndex(m)
		if loc == nil {
			return m
		}
		prefix := m[:loc[1]]
		// Check whether we're inside a string by counting unescaped quotes
		// before this match in the full string.
		idx := strings.Index(s, prefix)
		if idx < 0 {
			return m
		}
		if isInsideString(s, idx) {
			return m
		}
		return unquotedKeyRe.ReplaceAllString(m, `${1}"${2}":`)
	})
}

// isInsideString reports whether position pos in s is inside a JSON string.
func isInsideString(s string, pos int) bool {
	inString := false
	escape := false
	for i := 0; i < pos && i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
		}
	}
	return inString
}

var trailingCommaRe = regexp.MustCompile(`,(\s*[}\]])`)

func fixTrailingCommas(s string) string {
	return trailingCommaRe.ReplaceAllStringFunc(s, func(m string) string {
		loc := trailingCommaRe.FindStringIndex(m)
		if loc == nil {
			return m
		}
		prefix := m[:loc[1]]
		idx := strings.Index(s, prefix)
		if idx < 0 {
			return m
		}
		if isInsideString(s, idx) {
			return m
		}
		return trailingCommaRe.ReplaceAllString(m, "$1")
	})
}

// balanceBracketsSmart balances structural { } and [ ] only when the JSON
// is obviously truncated. It ignores braces inside strings.
func balanceBracketsSmart(s string) string {
	var braceDepth, bracketDepth int
	inString := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		}
	}
	for braceDepth > 0 {
		s += "}"
		braceDepth--
	}
	for bracketDepth > 0 {
		s += "]"
		bracketDepth--
	}
	return s
}
