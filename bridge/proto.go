// Package bridge defines the wire protocol for DMR local worker connections.
package bridge

const (
	ProtocolVersion = 1
	DefaultPrefix   = "local"
)

// Message type constants.
const (
	TypeHello          = "hello"
	TypeRegister       = "register"
	TypeRegisterAck    = "register_ack"
	TypeToolsUpdate    = "tools_update"
	TypeToolsUpdateAck = "tools_update_ack"
	TypeCall           = "call"
	TypeResult         = "result"
	TypePing           = "ping"
	TypePong           = "pong"
	TypeError          = "error"
)

// Route identifies the local plugin handler for a bridged tool.
type Route struct {
	Plugin       string `json:"plugin"`
	OriginalName string `json:"original_name"`
}

// ToolDef describes a tool registered by a worker.
type ToolDef struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	ParametersJSON string `json:"parameters_json"`
	Group          string `json:"group,omitempty"`
	SearchHint     string `json:"search_hint,omitempty"`
	AlwaysLoad     bool   `json:"always_load,omitempty"`
	Route          Route  `json:"route"`
}

// Frame is the top-level WebSocket message envelope.
type Frame struct {
	Type string `json:"type"`

	// hello (server → client)
	ProtocolVersion int      `json:"protocol_version,omitempty"`
	Caps            []string `json:"caps,omitempty"`

	// register / tools_update (client → server)
	WorkerID  string            `json:"worker_id,omitempty"`
	Hostname  string            `json:"hostname,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Tools     []ToolDef         `json:"tools,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
	Rev       int64             `json:"rev,omitempty"`

	// register_ack / tools_update_ack (server → client)
	Accepted bool     `json:"accepted,omitempty"`
	Rejected []string `json:"rejected,omitempty"`

	// call (server → client)
	CallID       string `json:"call_id,omitempty"`
	Tool         string `json:"tool,omitempty"`
	ArgsJSON     string `json:"args_json,omitempty"`
	SessionTape  string `json:"session_tape,omitempty"`
	ContextJSON  string `json:"context_json,omitempty"`
	TimeoutMs    int64  `json:"timeout_ms,omitempty"`
	OriginalTool string `json:"original_tool,omitempty"`

	// result (client → server)
	ResultJSON string `json:"result_json,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`

	// error (either direction)
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
