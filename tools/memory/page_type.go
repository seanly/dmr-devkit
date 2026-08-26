package memory

import (
	"fmt"
	"strings"
)

var allowedPageTypes = map[string]struct{}{
	"note":       {},
	"person":     {},
	"company":    {},
	"meeting":    {},
	"concept":    {},
	"config":     {},
	"episode":    {},
	"semantic":   {},
	"procedural": {},
}

// ValidatePageType checks a non-empty page type string. Empty means "use backend default".
func ValidatePageType(typ string) error {
	if strings.TrimSpace(typ) == "" {
		return nil
	}
	t := strings.ToLower(strings.TrimSpace(typ))
	if _, ok := allowedPageTypes[t]; !ok {
		return fmt.Errorf("invalid page type %q: allowed values are note, person, company, meeting, concept, config, episode, semantic, procedural", typ)
	}
	return nil
}
