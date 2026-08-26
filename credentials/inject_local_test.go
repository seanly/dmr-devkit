package credentials

import (
	"context"
	"testing"
)

func TestInjectLocalBindings_Noop(t *testing.T) {
	if err := InjectLocalBindings(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{}
	if err := InjectLocalBindings(context.Background(), nil, args); err != nil {
		t.Fatal(err)
	}
	if _, ok := args["credential_bindings"]; ok {
		t.Fatal("should not invent bindings")
	}
}
