package devkit

import (
	"encoding/json"
	"testing"

	"github.com/seanly/dmr-devkit/agent"
)

func TestMergeAgentContext(t *testing.T) {
	t.Parallel()
	raw, err := mergeAgentContext("be brief", `{"_dmr_prompt_parts":[{"type":"image_url"}],"system_prompt_override":"from json"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got[agent.ContextKeySystemPromptOverride] != "be brief" {
		t.Fatalf("system prompt = %v", got[agent.ContextKeySystemPromptOverride])
	}
	if _, ok := got["_dmr_prompt_parts"]; !ok {
		t.Fatalf("missing prompt parts: %s", raw)
	}

	empty, err := mergeAgentContext("", "")
	if err != nil || empty != "" {
		t.Fatalf("empty = %q err=%v", empty, err)
	}
	if _, err := mergeAgentContext("", "{"); err == nil {
		t.Fatal("expected invalid json error")
	}
}
