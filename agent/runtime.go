package agent

import "context"

// InterceptResult is returned by InterceptInput hook to short-circuit the agent loop.
type InterceptResult struct {
	Output     string // command output text
	Kind       string // "command" for renderer styling
	SwitchTape string // non-empty: caller should switch to this tape for the next run
}

// ModelInfo describes a configured model.
type ModelInfo struct {
	Name  string
	Model string
}

// ToolCallEvent carries info about a tool call for display.
type ToolCallEvent struct {
	Name      string
	Arguments string
	Result    string
}

// RunResult is the outcome of an agent run.
type RunResult struct {
	Output           string
	Steps            int
	PromptTokens     int
	CompletionTokens int
	SwitchTape       string // non-empty: caller should switch to this tape for the next run
}

// RuntimeAgent is the interface that plugins use to interact with the agent.
// It lives in pkg/agent so the agent loop and embedders do not depend on pkg/plugin.
type RuntimeAgent interface {
	// Model management (used by command plugin)
	AllModelInfos() []ModelInfo
	GetCurrentModelName(tapeName string) (name, model string)
	SwitchModel(tapeName, modelName string) error
	CompactTape(ctx context.Context, tapeName string) (summary string, err error)
	RestartProcess() error

	// Agent execution (used by cli plugin)
	Run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32) (*RunResult, error)
	SetOnToolCall(fn func(ToolCallEvent))

	// Sub-agent execution (used by subagent plugin)
	RunSubagent(ctx context.Context, parentTape, prompt, modelName, session, contextJSON string, maxSteps int) (string, error)

	// Sub-agent execution with tool whitelist.
	RunSubagentWithTools(ctx context.Context, parentTape, prompt, modelName, session, contextJSON string, maxSteps int, allowedTools []string) (string, error)

	// SetDefaultTape sets the canonical tape name for this session (used by CLI
	// so that ,tape.switch commands resolve relative to a stable key).
	SetDefaultTape(tape string)
}

// InterceptInputArgs holds the typed arguments for the InterceptInput hook.
type InterceptInputArgs struct {
	TapeName     string
	Prompt       string
	Workspace    string
	TapeStore    any // tape.TapeStore — uses any to avoid import cycles in plugins
	TapeManager  any // *tape.TapeManager
	RuntimeAgent RuntimeAgent
	TapeControl  any // plugin.TapeControl — uses any to avoid import cycles in plugins
	DefaultTape  string // canonical session tape; empty means same as TapeName
}

// AfterAgentRunArgs holds typed arguments for AfterAgentRun hook handlers.
type AfterAgentRunArgs struct {
	TapeName       string
	Turn           int
	ToolIterations int
}

// Result is an alias exposed by [Agent.Run].
type Result = RunResult
