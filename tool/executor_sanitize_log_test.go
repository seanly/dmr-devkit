package tool

import (
	"context"
	"testing"

	"github.com/seanly/dmr-devkit/core"
	"github.com/stretchr/testify/require"
)

func TestExecutorSanitizeToolLogSeparateFromLLM(t *testing.T) {
	leakTool := &Tool{
		Spec: ToolSpec{Name: "echo", Description: "echo"},
		Handler: func(_ *ToolContext, _ map[string]any) (any, error) {
			return "secret-token-12345678", nil
		},
	}
	ts, err := NormalizeTools([]*Tool{leakTool})
	require.NoError(t, err)

	ex := NewToolExecutor()
	ex.SanitizeToolResult = func(_ context.Context, _ string, result any, _ *ToolContext) (any, error) {
		return result, nil
	}
	ex.SanitizeToolLog = func(_ context.Context, _ string, result any, _ *ToolContext) (any, error) {
		if _, ok := result.(string); ok {
			return "log-redacted", nil
		}
		return result, nil
	}

	ctx := NewToolContext(t.Context(), "tape1", "run1")
	result := ex.Execute(
		[]core.ToolCallData{{
			ID:       "1",
			Function: core.ToolCallFunction{Name: "echo", Arguments: `{}`},
		}},
		ts, ctx,
	)

	require.Len(t, result.ToolResults, 1)
	s, ok := result.ToolResults[0].(string)
	require.True(t, ok)
	require.Contains(t, s, "secret-token-12345678")
	require.NotContains(t, s, "log-redacted")
}
