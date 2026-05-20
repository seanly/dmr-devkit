package a2aserver

import (
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestContextJSON_roundTrip(t *testing.T) {
	t.Parallel()
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("do task"))
	AttachContextMetadata(msg, map[string]any{
		"from":        "ceo",
		"reply_to":    "ceo",
		"workorder":   "wo-1",
		"feishu_chat": "oc_x",
	})
	got := ContextJSONFromMessage(msg)
	if got == "" {
		t.Fatal("empty context json")
	}
	for _, sub := range []string{`"from"`, `"ceo"`, `"wo-1"`} {
		if !strings.Contains(got, sub) {
			t.Fatalf("context %q missing %q", got, sub)
		}
	}
}
