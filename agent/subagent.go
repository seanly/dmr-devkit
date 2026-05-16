package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tape"
)

// SubagentTapeSuffix is appended to the parent tape name to form the child tape (parent + ":" + SubagentTapeSuffix).
const SubagentTapeSuffix = "subagent"

// subagentMaxSteps caps steps for a delegated subagent run (bounded cost per tool call).
const subagentMaxSteps = 12

// subagentMaxDepth limits how many levels of subagent nesting are allowed.
const subagentMaxDepth = 3

// RunSubagent runs a synchronous sub-task on tape "{parentTape}:subagent" with a fresh job anchor.
// Nested calls are allowed up to subagentMaxDepth.
// modelName: empty uses the agent's current chat model; non-empty switches only for the duration of this run.
// session: "temp" (default) scopes LLM context to entries after this job's anchor; "inherit" uses the full child tape.
// contextJSON: optional JSON string injected as a system message on the child tape.
// maxSteps: optional step cap (0 means fall back to subagentMaxSteps scaled by depth).
func (a *Agent) RunSubagent(ctx context.Context, parentTape, prompt, modelName, session, contextJSON string, maxSteps int) (string, error) {
	return a.RunSubagentWithTools(ctx, parentTape, prompt, modelName, session, contextJSON, maxSteps, nil)
}

// RunSubagentWithTools runs a sub-agent with an optional tool whitelist.
func (a *Agent) RunSubagentWithTools(ctx context.Context, parentTape, prompt, modelName, session, contextJSON string, maxSteps int, allowedTools []string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", core.NewError(core.ErrInvalidInput, "subagent: empty prompt", nil)
	}

	depth := countSubagentDepth(parentTape)
	if depth >= subagentMaxDepth {
		return "", core.NewError(core.ErrDenied, fmt.Sprintf("subagent: max nesting depth %d reached", subagentMaxDepth), nil)
	}

	jobID := newSubagentJobID()
	childTape := parentTape + ":" + SubagentTapeSuffix + ":" + shortJobID(jobID)
	a.Handoff(childTape, jobID, map[string]any{
		"parent_tape": parentTape,
		"kind":        "subagent_job",
	})

	var tc *tape.TapeContext
	switch strings.ToLower(strings.TrimSpace(session)) {
	case "inherit":
		tc = tape.NewNoAnchorContext()
	default:
		tc = tape.NewNamedAnchorContext(jobID)
	}

	// Inject optional contextJSON as a system message.
	if strings.TrimSpace(contextJSON) != "" {
		contextEntry := tape.NewSystemEntry(fmt.Sprintf("[Context from parent task]\n%s", contextJSON))
		if err := a.tape.Store.Append(childTape, contextEntry); err != nil {
			return "", core.NewError(core.ErrUnknown, "subagent: failed to append context", err)
		}
	}

	a.mu.RLock()
	prevChildOverride, hadChildOverride := a.modelOverrides[childTape]
	a.mu.RUnlock()
	defer func() {
		// Clean up per-tape ChatClient for subagent
		a.mu.Lock()
		delete(a.chatClients, childTape)
		if hadChildOverride {
			a.modelOverrides[childTape] = prevChildOverride
		} else {
			delete(a.modelOverrides, childTape)
		}
		a.mu.Unlock()
	}()

	if strings.TrimSpace(modelName) != "" {
		if err := a.SwitchModel(childTape, modelName); err != nil {
			kind := core.ErrUnknown
			var re *core.RepublicError
			if errors.As(err, &re) {
				kind = re.Kind
			}
			return "", core.NewError(kind, "subagent: model switch failed", err)
		}
	}

	// Scale maxSteps down by nesting depth to prevent cost explosion.
	effectiveMaxSteps := subagentMaxSteps
	if depth > 0 {
		effectiveMaxSteps = max(4, subagentMaxSteps/(depth+1))
	}
	if maxSteps > 0 && maxSteps < effectiveMaxSteps {
		effectiveMaxSteps = maxSteps
	}

	mode := &runMode{
		tapeContextOverride: tc,
		maxSteps:            effectiveMaxSteps,
	}
	// Only ban recursive subagent when we've hit max depth.
	if depth+1 >= subagentMaxDepth {
		mode.excludeToolNames = map[string]struct{}{"subagent": {}}
	}

	if len(allowedTools) > 0 {
		mode.allowedToolNames = make(map[string]struct{}, len(allowedTools))
		for _, name := range allowedTools {
			mode.allowedToolNames[name] = struct{}{}
		}
	}

	res, _, err := a.run(ctx, childTape, prompt, 0, mode, "")
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.Output, nil
}

func countSubagentDepth(tapeName string) int {
	return strings.Count(tapeName, ":"+SubagentTapeSuffix+":")
}

func newSubagentJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("subagent/job/fallback-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("subagent/job/%x", b[:])
}

// shortJobID extracts a short identifier from a full job ID for use in tape names.
// "subagent/job/abc123def456..." → "abc123"
func shortJobID(fullID string) string {
	parts := strings.Split(fullID, "/")
	if len(parts) >= 3 && len(parts[2]) >= 6 {
		return parts[2][:6]
	}
	if len(parts) >= 3 {
		return parts[2]
	}
	return fullID
}
