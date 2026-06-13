package agent

import (
	"context"
	"testing"
)

func TestContextWithTapeName(t *testing.T) {
	ctx := ContextWithTapeName(context.Background(), "feishu:p2p:abc")
	if got := TapeNameFromContext(ctx); got != "feishu:p2p:abc" {
		t.Errorf("TapeNameFromContext() = %q, want feishu:p2p:abc", got)
	}
	if got := TapeNameFromContext(context.Background()); got != "" {
		t.Errorf("TapeNameFromContext(empty ctx) = %q, want empty", got)
	}
	if got := TapeNameFromContext(nil); got != "" {
		t.Errorf("TapeNameFromContext(nil) = %q, want empty", got)
	}
}
