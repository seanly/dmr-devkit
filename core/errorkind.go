package core

// RecoveryAction tells the caller what to do after this error.
type RecoveryAction int

const (
	// RecoveryRetry — transient failure; same operation may succeed.
	RecoveryRetry RecoveryAction = iota
	// RecoveryRetryExponential — rate limit or temporary unavailability; back off.
	RecoveryRetryExponential
	// RecoveryFallback — try next model / provider in chain.
	RecoveryFallback
	// RecoveryHandoff — context overflow; compact and continue.
	RecoveryHandoff
	// RecoveryStop — non-recoverable; stop the current agent run.
	RecoveryStop
	// RecoveryCancel — external cancellation; stop gracefully.
	RecoveryCancel
	// RecoveryDelegate — subagent failed; parent decides whether to continue.
	RecoveryDelegate
)

// ErrorPhase identifies the pipeline stage where the error originated.
type ErrorPhase string

const (
	PhaseIntercept    ErrorPhase = "intercept"
	PhaseLLMCall      ErrorPhase = "llm_call"
	PhaseToolResolve  ErrorPhase = "tool_resolve"
	PhaseToolPolicy   ErrorPhase = "tool_policy"
	PhaseToolExecute  ErrorPhase = "tool_execute"
	PhaseCompact      ErrorPhase = "compact"
	PhaseHandoff      ErrorPhase = "handoff"
	PhaseSubagent     ErrorPhase = "subagent"
	PhaseWorkflow     ErrorPhase = "workflow"
	PhasePluginInit   ErrorPhase = "plugin_init"
	PhaseGateway      ErrorPhase = "gateway"
)

// ErrKind is a fine-grained error kind used across DMR.
// It is a superset of ErrorKind for backward compatibility.
type ErrKind string

const (
	ErrKindInvalidInput      ErrKind = "invalid_input"
	ErrKindConfig            ErrKind = "config"
	ErrKindProvider          ErrKind = "provider"
	ErrKindTool              ErrKind = "tool"
	ErrKindTemporary         ErrKind = "temporary"
	ErrKindNotFound          ErrKind = "not_found"
	ErrKindDenied            ErrKind = "denied"
	ErrKindUnknown           ErrKind = "unknown"
	ErrKindRateLimit         ErrKind = "rate_limit"
	ErrKindContextOverflow   ErrKind = "context_overflow"
	ErrKindTimeout           ErrKind = "timeout"
	ErrKindAuth              ErrKind = "auth"
	ErrKindNetwork           ErrKind = "network"
	ErrKindInterrupt         ErrKind = "interrupt"
	ErrKindPluginCrash       ErrKind = "plugin_crash"
	ErrKindApprovalRequired  ErrKind = "approval_required"
)

// recoveryMap decides the default recovery action for each kind.
var recoveryMap = map[ErrKind]RecoveryAction{
	ErrKindRateLimit:        RecoveryRetryExponential,
	ErrKindTimeout:          RecoveryRetry,
	ErrKindTemporary:        RecoveryRetry,
	ErrKindNetwork:          RecoveryRetry,
	ErrKindContextOverflow:  RecoveryHandoff,
	ErrKindInterrupt:        RecoveryCancel,
	ErrKindAuth:             RecoveryStop,
	ErrKindConfig:           RecoveryStop,
	ErrKindPluginCrash:      RecoveryFallback,
	ErrKindApprovalRequired: RecoveryStop,
	ErrKindDenied:           RecoveryDelegate,
	ErrKindInvalidInput:     RecoveryStop,
	ErrKindNotFound:         RecoveryDelegate,
	ErrKindTool:             RecoveryDelegate,
	ErrKindProvider:         RecoveryFallback,
	ErrKindUnknown:          RecoveryRetry,
}

// RecoveryFor returns the recommended recovery action for a kind.
func RecoveryFor(k ErrKind) RecoveryAction {
	if a, ok := recoveryMap[k]; ok {
		return a
	}
	return RecoveryRetry
}
