package agent

import (
	"context"
	"errors"

	"github.com/seanly/dmr-devkit/tape"
)

// ErrRunPreempted is the context cancel cause used when a channel queue preempts
// an in-flight agent run (e.g. Feishu PolicyPreempt). Matched via context.Cause.
var ErrRunPreempted = errors.New("dmr-devkit: agent run preempted")

const (
	InterruptReasonCancelled = "cancelled"
	InterruptReasonPreempted   = "preempted"
)

func interruptEventData(execID string, ctx context.Context) map[string]any {
	data := map[string]any{"exec_id": execID}
	if errors.Is(context.Cause(ctx), ErrRunPreempted) {
		data["reason"] = InterruptReasonPreempted
		data["suppress_notice"] = true
		return data
	}
	if ctx.Err() != nil {
		data["reason"] = InterruptReasonCancelled
	}
	return data
}

func (a *Agent) recordRunInterruptedIfNeeded(ctx context.Context, tapeName, execID string, recorded *bool) {
	if ctx == nil || ctx.Err() == nil {
		return
	}
	if recorded != nil && *recorded {
		return
	}
	if a == nil || a.tape == nil {
		return
	}
	_ = a.tape.AppendEntry(tapeName, tape.NewEventEntry(tape.EventRunInterrupted, interruptEventData(execID, ctx)))
	if recorded != nil {
		*recorded = true
	}
}
