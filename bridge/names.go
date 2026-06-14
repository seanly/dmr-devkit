package bridge

import (
	"strings"
)

// FormatToolName builds the cloud-visible tool name: {prefix}_{workerID}_{original}.
func FormatToolName(prefix, workerID, original string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = DefaultPrefix
	}
	workerID = strings.TrimSpace(workerID)
	original = strings.TrimSpace(original)
	return prefix + "_" + workerID + "_" + original
}

// ParseToolName splits a full bridged tool name into workerID and original tool name.
func ParseToolName(prefix, fullName string) (workerID, original string, ok bool) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = DefaultPrefix
	}
	head := prefix + "_"
	if !strings.HasPrefix(fullName, head) {
		return "", "", false
	}
	rest := strings.TrimPrefix(fullName, head)
	idx := strings.Index(rest, "_")
	if idx <= 0 || idx >= len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}
