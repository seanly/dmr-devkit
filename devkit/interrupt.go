package devkit

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/workflow"
)

// Interrupt exposes workflow.Interrupt to devkit consumers.
func Interrupt(wctx *workflow.Context, value any) (any, error) {
	return workflow.Interrupt(wctx, value)
}

// IsInterrupt reports whether err (or any error in its chain) is an *workflow.InterruptError.
func IsInterrupt(err error) bool {
	return workflow.IsInterrupt(err)
}

// ResumeWorkflow resumes an interrupted workflow.
// wctx must be the exact context from the interrupted run (preserves StepLog + State).
func (k *Kit) ResumeWorkflow(ctx context.Context, runner workflow.Runner, wctx *workflow.Context, resumeValue any, input any) (*workflow.Result, error) {
	if wctx == nil {
		return nil, fmt.Errorf("devkit: wctx is required for resume")
	}
	if wctx.Metadata == nil {
		wctx.Metadata = make(map[string]any)
	}
	if _, ok := wctx.Metadata["kit"]; !ok {
		wctx.Metadata["kit"] = k
		wctx.Metadata["tape_manager"] = k.TapeManager
		wctx.Metadata["agent"] = k.Agent
		wctx.Metadata["hooks"] = k.Hooks
	}
	wctx.ResumeData = resumeValue
	wctx.Step = 0
	out, err := runner.Run(ctx, wctx, input)
	res, ok := out.(*workflow.Result)
	if !ok {
		return nil, fmt.Errorf("devkit: workflow runner returned %T, expected *workflow.Result", out)
	}
	return res, err
}
