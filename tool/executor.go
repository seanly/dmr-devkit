package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/seanly/dmr-devkit/core"
)

// BeforeToolCallFunc is a hook called before tool execution.
// Returns non-nil error to deny the tool call.
type BeforeToolCallFunc func(ctx context.Context, t *Tool, args map[string]any, toolCtx *ToolContext) error

// BatchBeforeToolCallFunc is a hook called before batch tool execution.
// Returns a map of call index -> error for denied calls. nil means all approved.
type BatchBeforeToolCallFunc func(ctx context.Context, items []BatchCheckItem) map[int]error

// BatchCheckItem holds a resolved tool call for batch policy check.
type BatchCheckItem struct {
	Tool *Tool
	Args map[string]any
	Ctx  *ToolContext
}

// ToolExecutor executes tool calls against a ToolSet.
type ToolExecutor struct {
	BeforeToolCall      BeforeToolCallFunc
	BatchBeforeToolCall BatchBeforeToolCallFunc
	// Verbose mirrors config verbose: when >= 1, log each tool invocation (args + result, truncated by level).
	Verbose int
}

// resolvedCall holds a validated and parsed tool call.
type resolvedCall struct {
	tool *Tool
	args map[string]any
	err  *core.ErrorPayload
}

func NewToolExecutor() *ToolExecutor {
	return &ToolExecutor{}
}

// Execute processes a list of tool calls and returns the execution results.
func (e *ToolExecutor) Execute(
	calls []core.ToolCallData,
	toolSet *ToolSet,
	ctx *ToolContext,
) *core.ToolExecution {
	result := &core.ToolExecution{
		ToolCalls:   calls,
		ToolResults: make([]any, 0, len(calls)),
	}

	// Phase 1: resolve all calls (validate tool, parse args)
	resolved := make([]resolvedCall, len(calls))

	for i, call := range calls {
		t, ok := toolSet.Runnable[call.Function.Name]
		if !ok {
			resolved[i].err = &core.ErrorPayload{
				Kind:    core.ErrNotFound,
				Message: fmt.Sprintf("tool %q not found", call.Function.Name),
			}
			continue
		}
		if t.Handler == nil {
			resolved[i].err = &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q is schema-only (no handler)", call.Function.Name),
			}
			continue
		}
		if t.NeedContext && ctx == nil {
			resolved[i].err = &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("tool %q requires a ToolContext but none was provided", call.Function.Name),
			}
			continue
		}
		args, err := parseArgs(call.Function.Arguments)
		if err != nil {
			resolved[i].err = &core.ErrorPayload{
				Kind:    core.ErrInvalidInput,
				Message: fmt.Sprintf("failed to parse arguments for %q: %v", call.Function.Name, err),
			}
			continue
		}
		if args == nil {
			args = map[string]any{}
		}
		if schemaErr := validateParameters(t, args); schemaErr != nil {
			resolved[i].err = schemaErr
			continue
		}
		if pathVal, ok := args["path"].(string); ok && !filepath.IsAbs(pathVal) {
			if ws := ctx.GetCwd(); ws != "" {
				args["path"] = filepath.Join(ws, pathVal)
			}
		}
		resolved[i].tool = t
		resolved[i].args = args
	}

	// Phase 2: batch policy check
	denied := e.batchPolicyCheck(resolved, ctx)

	// Phase 3: execute
	argLimit, resLimit := toolLogStringLimits(e.Verbose)

	// Identify subagent calls that can run in parallel
	var subagentIndices []int
	for i, call := range calls {
		if resolved[i].err == nil && call.Function.Name == "subagent" {
			if _, ok := denied[i]; !ok {
				subagentIndices = append(subagentIndices, i)
			}
		}
	}

	// If multiple subagent calls can run in parallel, use fan-out/gather; otherwise serial
	if len(subagentIndices) > 1 {
		e.executeWithParallelSubagents(calls, resolved, denied, subagentIndices, ctx, result, argLimit, resLimit)
	} else {
		e.executeSerial(calls, resolved, denied, ctx, result, argLimit, resLimit)
	}

	return result
}

// executeSerial runs all tool calls sequentially (original behavior).
func (e *ToolExecutor) executeSerial(
	calls []core.ToolCallData,
	resolved []resolvedCall,
	denied map[int]error,
	ctx *ToolContext,
	result *core.ToolExecution,
	argLimit, resLimit int,
) {
	for i, call := range calls {
		name := call.Function.Name
		rc := resolved[i]
		if rc.err != nil {
			if e.Verbose >= 1 {
				slog.Info("tool skipped", "tool", name, "reason", rc.err.Message)
			}
			result.ToolResults = append(result.ToolResults, map[string]any{
				"kind":    string(rc.err.Kind),
				"message": rc.err.Message,
			})
			result.Error = rc.err
			continue
		}
		if e.Verbose >= 1 {
			slog.Info("tool call", "tool", name, "args", toolArgsJSON(rc.args, argLimit))
		}
		if err, ok := denied[i]; ok {
			if e.Verbose >= 1 {
				slog.Info("tool denied", "tool", name, "error", err)
			}
			ep := errorPayloadForTool(name, err, core.ErrDenied)
			result.ToolResults = append(result.ToolResults, map[string]any{
				"kind":    string(ep.Kind),
				"message": ep.Message,
			})
			result.Error = ep
			continue
		}
		out, err := rc.tool.Handler(ctx, rc.args)
		if err != nil {
			if e.Verbose >= 1 {
				se, ok := core.AsStructured(err)
				if ok {
					slog.Info("tool handler error", "tool", name, "kind", se.Kind, "error", err)
				} else {
					slog.Info("tool handler error", "tool", name, "error", err)
				}
			}
			ep := errorPayloadForTool(name, err, core.ErrTool)
			result.ToolResults = append(result.ToolResults, map[string]any{
				"kind":    string(ep.Kind),
				"message": ep.Message,
			})
			result.Error = ep
			continue
		}
		if e.Verbose >= 1 {
			slog.Info("tool call ok", "tool", name, "result", toolResultLogString(out, resLimit))
		}
		result.ToolResults = append(result.ToolResults, tagToolResult(name, out))
	}
}

// executeWithParallelSubagents runs subagent calls in parallel while other calls run serially.
// Non-subagent calls before/between/after subagent calls are executed in order.
// All subagent calls are fan-out concurrently, and the method waits for all to complete before
// filling results in the original call order.
func (e *ToolExecutor) executeWithParallelSubagents(
	calls []core.ToolCallData,
	resolved []resolvedCall,
	denied map[int]error,
	subagentIndices []int,
	ctx *ToolContext,
	result *core.ToolExecution,
	argLimit, resLimit int,
) {
	subagentSet := make(map[int]struct{}, len(subagentIndices))
	for _, idx := range subagentIndices {
		subagentSet[idx] = struct{}{}
	}

	// Fan-out: launch all subagent calls in parallel
	type parallelResult struct {
		index int
		out   any
		err   error
	}

	var wg sync.WaitGroup
	resultCh := make(chan parallelResult, len(subagentIndices))

	for _, idx := range subagentIndices {
		rc := resolved[idx]
		name := calls[idx].Function.Name
		if e.Verbose >= 1 {
			slog.Info("tool call", "tool", name, "mode", "parallel", "args", toolArgsJSON(rc.args, argLimit))
		}
		wg.Add(1)
		go func(i int, rc resolvedCall) {
			defer wg.Done()

			// Clone args and ctx for concurrency safety
			safeArgs := cloneArgs(rc.args)
			safeCtx := cloneToolContext(ctx)

			done := make(chan struct{})
			var out any
			var err error
			go func() {
				out, err = rc.tool.Handler(safeCtx, safeArgs)
				close(done)
			}()
			select {
			case <-done:
				resultCh <- parallelResult{index: i, out: out, err: err}
			case <-ctx.Ctx.Done():
				resultCh <- parallelResult{index: i, out: nil, err: ctx.Ctx.Err()}
			}
		}(idx, rc)
	}

	// Gather: wait for all subagent goroutines
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	subagentResults := make(map[int]parallelResult)
	for r := range resultCh {
		subagentResults[r.index] = r
	}

	// Fill results in original call order
	for i, call := range calls {
		name := call.Function.Name
		rc := resolved[i]

		// Handle resolve errors
		if rc.err != nil {
			if e.Verbose >= 1 {
				slog.Info("tool skipped", "tool", name, "reason", rc.err.Message)
			}
			result.ToolResults = append(result.ToolResults, map[string]any{
				"kind":    string(rc.err.Kind),
				"message": rc.err.Message,
			})
			result.Error = rc.err
			continue
		}

		// Handle denied calls
		if err, ok := denied[i]; ok {
			if e.Verbose >= 1 {
				slog.Info("tool denied", "tool", name, "error", err)
			}
			ep := errorPayloadForTool(name, err, core.ErrDenied)
			result.ToolResults = append(result.ToolResults, map[string]any{
				"kind":    string(ep.Kind),
				"message": ep.Message,
			})
			result.Error = ep
			continue
		}

		// Subagent call — use pre-collected parallel result
		if _, ok := subagentSet[i]; ok {
			r := subagentResults[i]
			if r.err != nil {
				if e.Verbose >= 1 {
					se, ok := core.AsStructured(r.err)
					if ok {
						slog.Info("tool handler error", "tool", name, "mode", "parallel", "kind", se.Kind, "error", r.err)
					} else {
						slog.Info("tool handler error", "tool", name, "mode", "parallel", "error", r.err)
					}
				}
				ep := errorPayloadForTool(name, r.err, core.ErrTool)
				result.ToolResults = append(result.ToolResults, map[string]any{
					"kind":    string(ep.Kind),
					"message": ep.Message,
				})
				result.Error = ep
			} else {
				if e.Verbose >= 1 {
					slog.Info("tool call ok", "tool", name, "mode", "parallel", "result", toolResultLogString(r.out, resLimit))
				}
				result.ToolResults = append(result.ToolResults, tagToolResult(name, r.out))
			}
			continue
		}

		// Non-subagent call — execute serially
		if e.Verbose >= 1 {
			slog.Info("tool call", "tool", name, "args", toolArgsJSON(rc.args, argLimit))
		}
		out, err := rc.tool.Handler(ctx, rc.args)
		if err != nil {
			if e.Verbose >= 1 {
				se, ok := core.AsStructured(err)
				if ok {
					slog.Info("tool handler error", "tool", name, "kind", se.Kind, "error", err)
				} else {
					slog.Info("tool handler error", "tool", name, "error", err)
				}
			}
			ep := errorPayloadForTool(name, err, core.ErrTool)
			result.ToolResults = append(result.ToolResults, map[string]any{
				"kind":    string(ep.Kind),
				"message": ep.Message,
			})
			result.Error = ep
			continue
		}
		if e.Verbose >= 1 {
			slog.Info("tool call ok", "tool", name, "result", toolResultLogString(out, resLimit))
		}
		result.ToolResults = append(result.ToolResults, tagToolResult(name, out))
	}
}

// toolLogStringLimits returns max runes per string field in args JSON, and max runes for result text, by verbose level.
func toolLogStringLimits(verbose int) (argMaxRunes, resultMaxRunes int) {
	switch {
	case verbose >= 3:
		return 32000, 100000
	case verbose >= 2:
		return 4096, 16384
	default:
		return 512, 2048
	}
}

func toolArgsJSON(args map[string]any, maxRunes int) string {
	if args == nil {
		return "{}"
	}
	tr := truncateToolValueForLog(args, maxRunes)
	b, err := json.Marshal(tr)
	if err != nil {
		return fmt.Sprintf("%v", tr)
	}
	return string(b)
}

func truncateToolValueForLog(v any, maxRunes int) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return truncateRunes(x, maxRunes)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			if k == "_runtime_env" || k == "_runtime_inject_tempfiles" || k == "credential_bindings" {
				out[k] = "(redacted)"
				continue
			}
			out[k] = truncateToolValueForLog(v, maxRunes)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = truncateToolValueForLog(x[i], maxRunes)
		}
		return out
	default:
		return v
	}
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + fmt.Sprintf(" …[truncated %d runes]", len(r)-maxRunes)
}

// tagToolResult adds a source marker to tool results so the model can distinguish
// live data from cached or inferred data. Only string results are prefixed;
// structured results are left as-is to preserve JSON parseability.
func tagToolResult(name string, out any) any {
	if out == nil {
		return out
	}
	switch x := out.(type) {
	case string:
		return "[LIVE DATA from " + name + "]\n" + x
	default:
		return out
	}
}

func toolResultLogString(out any, maxRunes int) string {
	if out == nil {
		return "null"
	}
	var s string
	switch x := out.(type) {
	case string:
		s = x
	case []byte:
		s = string(x)
	default:
		if b, err := json.Marshal(x); err == nil {
			s = string(b)
		} else {
			s = fmt.Sprint(x)
		}
	}
	return truncateRunes(s, maxRunes)
}

// batchPolicyCheck runs policy checks. Prefers batch hook, falls back to per-call.
func (e *ToolExecutor) batchPolicyCheck(resolved []resolvedCall, ctx *ToolContext) map[int]error {
	// Collect valid calls
	var items []BatchCheckItem
	var indices []int
	for i, rc := range resolved {
		if rc.err == nil {
			items = append(items, BatchCheckItem{Tool: rc.tool, Args: rc.args, Ctx: ctx})
			indices = append(indices, i)
		}
	}
	if len(items) == 0 {
		return nil
	}

	// Derive a context.Context for policy hooks from the ToolContext.
	goCtx := context.Background()
	if ctx != nil && ctx.Ctx != nil {
		goCtx = ctx.Ctx
	}

	// Try batch hook
	if e.BatchBeforeToolCall != nil {
		batchResult := e.BatchBeforeToolCall(goCtx, items)
		if batchResult == nil {
			return nil
		}
		// Remap batch indices to original indices
		denied := make(map[int]error)
		for batchIdx, err := range batchResult {
			denied[indices[batchIdx]] = err
		}
		return denied
	}

	// Fallback: per-call
	if e.BeforeToolCall == nil {
		return nil
	}
	denied := make(map[int]error)
	for j, item := range items {
		if err := e.BeforeToolCall(goCtx, item.Tool, item.Args, item.Ctx); err != nil {
			denied[indices[j]] = err
		}
	}
	return denied
}

func parseArgs(raw string) (map[string]any, error) {
	if raw == "" || raw == "{}" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return args, nil
}

func cloneArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	b, err := json.Marshal(args)
	if err != nil {
		// fallback to shallow clone if marshal fails (shouldn't happen for valid tool args)
		cloned := make(map[string]any, len(args))
		for k, v := range args {
			cloned[k] = v
		}
		return cloned
	}
	var cloned map[string]any
	_ = json.Unmarshal(b, &cloned)
	return cloned
}

// errorPayloadForTool converts any error into a core.ErrorPayload,
// preserving StructuredError kind and message when available.
func errorPayloadForTool(toolName string, err error, defaultKind core.ErrorKind) *core.ErrorPayload {
	if se, ok := core.AsStructured(err); ok {
		return &core.ErrorPayload{
			Kind:    core.ErrorKind(se.Kind),
			Message: se.Message,
		}
	}
	return &core.ErrorPayload{
		Kind:    defaultKind,
		Message: fmt.Sprintf("tool %q handler error: %v", toolName, err),
	}
}

func cloneToolContext(ctx *ToolContext) *ToolContext {
	if ctx == nil {
		return nil
	}
	cloned := *ctx
	// Shallow clone State map
	if ctx.State != nil {
		clonedState := make(map[string]any, len(ctx.State))
		for k, v := range ctx.State {
			clonedState[k] = v
		}
		cloned.State = clonedState
	}
	return &cloned
}
