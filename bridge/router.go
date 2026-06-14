package bridge

import (
	"fmt"
	"strings"

	"github.com/seanly/dmr-devkit/tool"
)

const (
	// WorkerIDArg is the tool argument that routes execution to a remote worker.
	WorkerIDArg = "worker_id"
	// WorkerLocal marks explicit cloud-local execution.
	WorkerLocal = "local"
)

// ResolveWorkerID returns the target worker for remote execution, or "" for cloud-local.
// Priority: args.worker_id, then toolCtx.Context["worker_id"].
func ResolveWorkerID(args map[string]any, toolCtx *tool.ToolContext) string {
	if wid := normalizeWorkerID(stringArg(args, WorkerIDArg)); wid != "" {
		return wid
	}
	if toolCtx != nil && toolCtx.Context != nil {
		if wid := normalizeWorkerID(stringArg(toolCtx.Context, WorkerIDArg)); wid != "" {
			return wid
		}
	}
	return ""
}

// IsRemoteExecution reports whether the call should run on a remote worker.
func IsRemoteExecution(args map[string]any, toolCtx *tool.ToolContext) bool {
	return ResolveWorkerID(args, toolCtx) != ""
}

// StripWorkerID removes routing fields from args before forwarding to a worker handler.
func StripWorkerID(args map[string]any) {
	if args == nil {
		return
	}
	delete(args, WorkerIDArg)
}

// SetBridgeContext annotates toolCtx with bridge metadata for OPA and approval UI.
func SetBridgeContext(toolCtx *tool.ToolContext, workerID, toolName, hostname, workspace string) {
	if toolCtx == nil {
		return
	}
	if toolCtx.Context == nil {
		toolCtx.Context = map[string]any{}
	}
	toolCtx.Context["bridge"] = map[string]any{
		"worker_id":     workerID,
		"hostname":      hostname,
		"original_tool": toolName,
		"online":        true,
		"workspace":     workspace,
	}
}

func normalizeWorkerID(wid string) string {
	wid = strings.TrimSpace(wid)
	if wid == "" || strings.EqualFold(wid, WorkerLocal) {
		return ""
	}
	return wid
}

func stringArg(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}
