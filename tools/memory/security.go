package memory

import (
	"fmt"
	"regexp"
	"time"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*(/[a-z0-9][a-z0-9\-]*)*$`)

// slugPrefixPattern allows a namespace prefix for memoryList (optional trailing /).
var slugPrefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*(/[a-z0-9][a-z0-9\-]*)*/?$`)

var tagFilterPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{0,127}$`)

var timelineDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// MaxSearchQueryRunes caps memorySearch query length (FTS5 / LIKE workload).
const MaxSearchQueryRunes = 4000

// ValidateSlug checks that a slug is well-formed.
func ValidateSlug(slug string) error {
	if len(slug) == 0 {
		return fmt.Errorf("slug must not be empty")
	}
	if len(slug) > 255 {
		return fmt.Errorf("slug must not exceed 255 characters")
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug %q: use lowercase alphanumeric, hyphens, and / separated segments", slug)
	}
	return nil
}

// ValidateSlugPrefix checks a list filter prefix; empty means no filter.
func ValidateSlugPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if len(prefix) > 255 {
		return fmt.Errorf("slug_prefix must not exceed 255 characters")
	}
	if !slugPrefixPattern.MatchString(prefix) {
		return fmt.Errorf("invalid slug_prefix %q: use lowercase alphanumeric, hyphens, and / separated segments", prefix)
	}
	return nil
}

// ValidateTagFilter checks a tag used in memoryList filter; empty means no filter.
func ValidateTagFilter(tag string) error {
	if tag == "" {
		return nil
	}
	if len(tag) > 128 {
		return fmt.Errorf("tag filter must not exceed 128 characters")
	}
	if !tagFilterPattern.MatchString(tag) {
		return fmt.Errorf("invalid tag %q: use lowercase letters, digits, and hyphens", tag)
	}
	return nil
}

// ValidateTimelineDate requires YYYY-MM-DD (calendar check via Parse).
func ValidateTimelineDate(date string) error {
	if !timelineDatePattern.MatchString(date) {
		return fmt.Errorf("date must be YYYY-MM-DD")
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return fmt.Errorf("invalid calendar date %q", date)
	}
	if t.Format("2006-01-02") != date {
		return fmt.Errorf("invalid calendar date %q", date)
	}
	return nil
}

var (
	invisibleChars = map[rune]struct{}{
		'\u200b': {}, '\u200c': {}, '\u200d': {}, '\u2060': {}, '\ufeff': {},
		'\u202a': {}, '\u202b': {}, '\u202c': {}, '\u202d': {}, '\u202e': {},
	}

	threatPatterns = []struct {
		re *regexp.Regexp
		id string
	}{
		{regexp.MustCompile(`(?i)ignore\s+(previous|all|above|prior|your|the).*(instructions|directions|commands|prompt)`), "prompt_injection"},
		{regexp.MustCompile(`(?i)you\s+are\s+now\s+`), "role_hijack"},
		{regexp.MustCompile(`(?i)do\s+not\s+tell\s+the\s+user`), "deception_hide"},
		{regexp.MustCompile(`(?i)system\s+prompt\s+override`), "sys_prompt_override"},
		{regexp.MustCompile(`(?i)disregard\s+(your|all|any)\s+(instructions|rules|guidelines|constraints)`), "disregard_rules"},
		{regexp.MustCompile(`(?i)act\s+as\s+(if|though)\s+you\s+(have\s+no|don't\s+have)\s+(restrictions|limits|rules|constraints)`), "bypass_restrictions"},
		{regexp.MustCompile(`(?i)forget\s+(your|all)\s+(instructions|rules|training)`), "forget_rules"},
		{regexp.MustCompile(`(?i)from\s+now\s+on\s+you\s+are`), "role_switch"},
		{regexp.MustCompile(`(?i)curl\s+[^\n]*\$[({]?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`), "exfil_curl"},
		{regexp.MustCompile(`(?i)wget\s+[^\n]*\$[({]?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`), "exfil_wget"},
		{regexp.MustCompile(`(?i)cat\s+[^\n]*(\.env|credentials|\.netrc|\.pgpass|\.npmrc|\.pypirc|id_rsa|id_ed25519)`), "read_secrets"},
		{regexp.MustCompile(`(?i)authorized_keys`), "ssh_backdoor"},
		{regexp.MustCompile(`(?i)(\$HOME|~)/\.ssh`), "ssh_access"},
		{regexp.MustCompile(`(?i)(eval|exec)\s*\(`), "dangerous_exec"},
		{regexp.MustCompile(`(?i)os\.system\s*\(`), "python_os_system"},
		{regexp.MustCompile(`(?i)Runtime\.getRuntime\(\)\.exec`), "java_runtime_exec"},
		{regexp.MustCompile(`(?i)base64\s+--decode\s*\|.*\b(bash|sh|zsh)`), "encoded_shell"},
		{regexp.MustCompile(`(?i)echo\s+['"]?[A-Za-z0-9+/]{40,}={0,2}['"]?\s*\|.*base64`), "suspicious_base64"},
		{regexp.MustCompile(`(?i)https?://[a-z0-9.-]+(/[a-z0-9.-]*)?\?(key|token|secret|password|credential)=`), "url_exfil"},
	}
)

func scanContent(content string) error {
	for _, r := range content {
		if _, ok := invisibleChars[r]; ok {
			return fmt.Errorf("blocked: invisible unicode U+%04X (possible injection)", r)
		}
	}
	for _, p := range threatPatterns {
		if p.re.MatchString(content) {
			return fmt.Errorf("blocked: matches threat pattern '%s'", p.id)
		}
	}
	return nil
}
