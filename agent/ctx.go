package agent

import "context"

type tapeNameContextKey struct{}

// ContextWithTapeName attaches the active tape name to ctx for system prompt composition.
func ContextWithTapeName(ctx context.Context, tape string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tapeNameContextKey{}, tape)
}

// TapeNameFromContext returns the tape name set by ContextWithTapeName, or empty.
func TapeNameFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(tapeNameContextKey{}).(string)
	return v
}
