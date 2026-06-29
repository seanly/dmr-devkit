package bridge

import "fmt"

// Error codes for bridge protocol and tool execution.
const (
	CodeWorkerOffline   = "worker_offline"
	CodeWorkerNotFound  = "worker_not_found"
	CodeCallTimeout     = "call_timeout"
	CodeAuthFailed      = "auth_failed"
	CodeDuplicateWorker = "duplicate_worker"
	CodeToolNotFound    = "tool_not_found"
)

// WorkerOfflineError is returned when a worker is not connected.
type WorkerOfflineError struct {
	WorkerID string
}

func (e *WorkerOfflineError) Error() string {
	if e.WorkerID != "" {
		return fmt.Sprintf("worker %q is offline; start `dmr forge worker` locally", e.WorkerID)
	}
	return "worker is offline; start `dmr forge worker` locally"
}

// WorkerNotFoundError is returned when no worker matches the tool route.
type WorkerNotFoundError struct {
	WorkerID string
}

func (e *WorkerNotFoundError) Error() string {
	if e.WorkerID != "" {
		return fmt.Sprintf("worker %q not found", e.WorkerID)
	}
	return "worker not found"
}

// ToolNotFoundError is returned when a tool is not registered on the worker.
type ToolNotFoundError struct {
	Tool string
}

func (e *ToolNotFoundError) Error() string {
	return fmt.Sprintf("tool %q not found on worker", e.Tool)
}

// WorkerUnsupportedToolError is returned when a worker does not support a tool.
type WorkerUnsupportedToolError struct {
	WorkerID string
	Tool     string
}

func (e *WorkerUnsupportedToolError) Error() string {
	return fmt.Sprintf("worker %q does not support tool %q", e.WorkerID, e.Tool)
}
