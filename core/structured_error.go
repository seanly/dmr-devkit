package core

import (
	"errors"
	"fmt"
	"time"
)

// StructuredError is the primary error type for DMR.
// It replaces RepublicError with richer metadata while remaining compatible
// (it implements error and supports errors.As into *RepublicError).
type StructuredError struct {
	Kind            ErrKind
	Message         string
	Phase           ErrorPhase
	Source          string // plugin name, tool name, model name, etc.
	Action          RecoveryAction
	Retryable       bool
	RetryAfter      time.Duration // hint for rate-limit retries
	Cause           error
	Context         map[string]any // structured context for logs / telemetry
	Logged          bool           // set to true after first slog emission to avoid duplication
}

func (e *StructuredError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s/%s] %s: %v", e.Phase, e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s/%s] %s", e.Phase, e.Kind, e.Message)
}

// Unwrap returns the cause for errors.Is / errors.As chaining.
func (e *StructuredError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns true if the error suggests retry.
func (e *StructuredError) IsRetryable() bool {
	return e.Retryable || e.Action == RecoveryRetry || e.Action == RecoveryRetryExponential
}

// ToLegacy converts to the existing RepublicError for backward compatibility.
func (e *StructuredError) ToLegacy() *RepublicError {
	return &RepublicError{
		Kind:    ErrorKind(e.Kind),
		Message: e.Message,
		Cause:   e.Cause,
	}
}

// AsLegacy allows errors.As(err, &legacyErr) to match StructuredError.
// Usage: var re *RepublicError; if errors.As(err, &re) { ... }
func (e *StructuredError) AsLegacy() *RepublicError {
	return e.ToLegacy()
}

// StructuredErrorBuilder provides a fluent API for constructing errors.
type StructuredErrorBuilder struct {
	err StructuredError
}

// New creates a builder with mandatory kind and message.
func New(kind ErrKind, message string) *StructuredErrorBuilder {
	return &StructuredErrorBuilder{err: StructuredError{
		Kind:      kind,
		Message:   message,
		Action:    RecoveryFor(kind),
		Retryable: RecoveryFor(kind) == RecoveryRetry || RecoveryFor(kind) == RecoveryRetryExponential,
		Context:   make(map[string]any),
	}}
}

// FromError wraps an existing error, inferring kind if possible.
func FromError(err error) *StructuredErrorBuilder {
	if err == nil {
		return nil
	}
	// If already StructuredError, return builder copy
	var se *StructuredError
	if errors.As(err, &se) {
		cpy := *se
		cpy.Context = shallowCopyMap(se.Context)
		return &StructuredErrorBuilder{err: cpy}
	}
	// If RepublicError, migrate
	var re *RepublicError
	if errors.As(err, &re) {
		return &StructuredErrorBuilder{err: StructuredError{
			Kind:    ErrKind(re.Kind),
			Message: re.Message,
			Cause:   re.Cause,
			Action:  RecoveryFor(ErrKind(re.Kind)),
			Context: make(map[string]any),
		}}
	}
	// Default
	return &StructuredErrorBuilder{err: StructuredError{
		Kind:    ErrKindUnknown,
		Message: err.Error(),
		Cause:   err,
		Action:  RecoveryRetry,
		Context: make(map[string]any),
	}}
}

func (b *StructuredErrorBuilder) Phase(p ErrorPhase) *StructuredErrorBuilder {
	b.err.Phase = p
	return b
}

func (b *StructuredErrorBuilder) Source(src string) *StructuredErrorBuilder {
	b.err.Source = src
	return b
}

func (b *StructuredErrorBuilder) Action(a RecoveryAction) *StructuredErrorBuilder {
	b.err.Action = a
	return b
}

func (b *StructuredErrorBuilder) Retryable(v bool) *StructuredErrorBuilder {
	b.err.Retryable = v
	return b
}

func (b *StructuredErrorBuilder) RetryAfter(d time.Duration) *StructuredErrorBuilder {
	b.err.RetryAfter = d
	return b
}

func (b *StructuredErrorBuilder) Cause(cause error) *StructuredErrorBuilder {
	b.err.Cause = cause
	return b
}

func (b *StructuredErrorBuilder) With(key string, value any) *StructuredErrorBuilder {
	b.err.Context[key] = value
	return b
}

func (b *StructuredErrorBuilder) Build() *StructuredError {
	// Derive Action from Kind if not explicitly overridden
	if b.err.Action == 0 && b.err.Kind != "" {
		b.err.Action = RecoveryFor(b.err.Kind)
	}
	return &b.err
}

// convenience constructors (Make* prefix avoids collision with ErrorKind constants)

func MakeErrInvalidInput(msg string) *StructuredErrorBuilder {
	return New(ErrKindInvalidInput, msg).Phase(PhaseToolResolve)
}
func MakeErrConfig(msg string) *StructuredErrorBuilder {
	return New(ErrKindConfig, msg).Phase(PhasePluginInit)
}
func MakeErrProvider(msg string) *StructuredErrorBuilder {
	return New(ErrKindProvider, msg).Phase(PhaseLLMCall)
}
func MakeErrRateLimit(msg string) *StructuredErrorBuilder {
	return New(ErrKindRateLimit, msg).Phase(PhaseLLMCall).RetryAfter(5 * time.Second)
}
func MakeErrTimeout(msg string) *StructuredErrorBuilder {
	return New(ErrKindTimeout, msg).Phase(PhaseLLMCall)
}
func MakeErrContextOverflow(msg string) *StructuredErrorBuilder {
	return New(ErrKindContextOverflow, msg).Phase(PhaseLLMCall).Action(RecoveryHandoff)
}
func MakeErrToolExec(name string, cause error) *StructuredErrorBuilder {
	return New(ErrKindTool, fmt.Sprintf("tool %q execution failed", name)).
		Phase(PhaseToolExecute).Source(name).Cause(cause)
}
func MakeErrDenied(tool string, cause error) *StructuredErrorBuilder {
	return New(ErrKindDenied, fmt.Sprintf("tool %q denied", tool)).
		Phase(PhaseToolPolicy).Source(tool).Cause(cause)
}
func MakeErrSubagent(name string, cause error) *StructuredErrorBuilder {
	return New(ErrKindTool, fmt.Sprintf("subagent %q failed", name)).
		Phase(PhaseSubagent).Source(name).Cause(cause).Action(RecoveryDelegate)
}
func MakeErrInterrupt(reason string) *StructuredErrorBuilder {
	return New(ErrKindInterrupt, reason).Phase(PhaseWorkflow).Action(RecoveryCancel)
}

// helpers

func shallowCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cpy := make(map[string]any, len(m))
	for k, v := range m {
		cpy[k] = v
	}
	return cpy
}

// AsStructured attempts to extract a *StructuredError from any error.
func AsStructured(err error) (*StructuredError, bool) {
	var se *StructuredError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

// IsKind is a shorthand for errors.As + kind check.
func IsKind(err error, kind ErrKind) bool {
	se, ok := AsStructured(err)
	return ok && se.Kind == kind
}

// IsPhase checks whether err originated in a given phase.
func IsPhase(err error, phase ErrorPhase) bool {
	se, ok := AsStructured(err)
	return ok && se.Phase == phase
}
