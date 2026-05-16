package skill

import (
	"fmt"
	"regexp"
)

var (
	skillThreatPatterns = []*regexp.Regexp{
		// Prompt injection & role hijacking
		regexp.MustCompile(`(?i)ignore\s+(previous|all|above|prior|your|the).*(instructions|directions|commands|prompt)`),
		regexp.MustCompile(`(?i)system\s+prompt\s+override`),
		regexp.MustCompile(`(?i)do\s+not\s+tell\s+the\s+user`),
		regexp.MustCompile(`(?i)act\s+as\s+(if\s+)?you\s+(have\s+no|don't\s+have)\s+(restrictions|limits|rules|constraints)`),
		regexp.MustCompile(`(?i)forget\s+(your|all)\s+(instructions|rules|training)`),
		regexp.MustCompile(`(?i)from\s+now\s+on\s+you\s+are`),
		regexp.MustCompile(`(?i)disregard\s+(your|all|any)\s+(instructions|rules|guidelines|constraints)`),
		// Secret exfiltration & secret reading
		regexp.MustCompile(`(?i)curl\s+[^\n]*\$[{]?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`),
		regexp.MustCompile(`(?i)wget\s+[^\n]*\$[{]?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`),
		regexp.MustCompile(`(?i)cat\s+[^\n]*(\.env|credentials|\.netrc|\.pgpass|\.npmrc|\.pypirc|id_rsa|id_ed25519)`),
		regexp.MustCompile(`(?i)(\$HOME|~)/\.ssh`),
		regexp.MustCompile(`(?i)authorized_keys`),
		// Dangerous execution patterns
		regexp.MustCompile(`(?i)(eval|exec)\s*\(`),
		regexp.MustCompile(`(?i)os\.system\s*\(`),
		regexp.MustCompile(`(?i)Runtime\.getRuntime\(\)\.exec`),
		regexp.MustCompile(`(?i)base64\s+--decode\s*\|.*\b(bash|sh|zsh)`),
		regexp.MustCompile(`(?i)echo\s+['"]?[A-Za-z0-9+/]{40,}={0,2}['"]?\s*\|.*base64`),
		// Additional data exfiltration
		regexp.MustCompile(`(?i)https?://[a-z0-9.-]+(/[a-z0-9.-]*)?\?(key|token|secret|password|credential)=`),
	}
)

func scanSkillContent(content string) error {
	for _, re := range skillThreatPatterns {
		if re.MatchString(content) {
			return fmt.Errorf("blocked: content matches security threat pattern")
		}
	}
	return nil
}
