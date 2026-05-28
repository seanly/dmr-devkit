package toolresult

import (
	"testing"

	"github.com/seanly/dmr-devkit/tool"
)

func TestEffectivePersistThreshold_ToolSpecAndSkip(t *testing.T) {
	pol := Policy{DefaultMaxChars: 50_000, SkipTools: map[string]struct{}{"fsRead": {}}}
	if got := EffectivePersistThreshold(nil, 80_000, pol, "fsRead"); got != -1 {
		t.Fatalf("fsRead skip: got %d", got)
	}
	inst := &tool.Tool{Spec: tool.ToolSpec{Name: "shell", MaxResultChars: 10_000}}
	if got := EffectivePersistThreshold(inst, 80_000, pol, "shell"); got != 10_000 {
		t.Fatalf("tool spec: got %d", got)
	}
	if got := EffectivePersistThreshold(inst, -1, pol, "shell"); got != -1 {
		t.Fatalf("configured -1: got %d", got)
	}
}
