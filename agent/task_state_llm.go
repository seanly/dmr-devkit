package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/handoff"
	"github.com/seanly/dmr-devkit/tape"
)

func (a *Agent) usesLLMStateExtract() bool {
	return a.handoffCfg().StateUpdate == "llm_extract"
}

func (a *Agent) updateTaskStateAfterToolRound(ctx context.Context, tapeName string, step int) {
	if !a.taskStateEnabled() {
		return
	}
	entries, err := a.fetchTapeEntries(tapeName)
	if err != nil {
		return
	}
	prev := handoff.LatestState(entries)
	var s handoff.State
	if a.usesLLMStateExtract() && a.defaultChat != nil {
		extracted, extractErr := a.extractTaskStateLLM(ctx, tapeName, prev, entries, step)
		if extractErr != nil {
			slog.Warn("task_state llm_extract failed, falling back to heuristic", "error", extractErr)
			s = a.stateUpdater().UpdateFromToolRound(prev, entries, step, "heuristic")
		} else {
			s = extracted
		}
	} else {
		s = a.stateUpdater().UpdateFromToolRound(prev, entries, step, "heuristic")
	}
	_ = a.appendTaskState(tapeName, s)
}

func (a *Agent) extractTaskStateLLM(ctx context.Context, tapeName string, prev *handoff.State, entries []tape.TapeEntry, step int) (handoff.State, error) {
	base := a.stateUpdater().UpdateFromToolRound(prev, entries, step, "llm_extract")
	prevJSON, _ := json.Marshal(prev)
	prompt := fmt.Sprintf(`Previous TaskState (start from this and update it):
%s

Recent tape activity since the previous state:
%s

Produce the updated TaskState v1 JSON. Remember: inherit the goal and active constraints unless the user explicitly changed them; preserve pending items unless they are completed or cancelled; mark completed work as done; record the latest last_action and active files.`,
		string(prevJSON), handoff.FormatRecentEntries(entries, 15))

	raw, err := a.extractTaskStateLLMRaw(ctx, tapeName, prompt, 2000)
	if err != nil {
		return base, err
	}
	out, err := handoff.ParseStateJSON(raw, base)
	if err != nil {
		// Retry once with a larger budget in case the JSON was truncated.
		raw, err = a.extractTaskStateLLMRaw(ctx, tapeName, prompt, 4000)
		if err != nil {
			return base, err
		}
		out, err = handoff.ParseStateJSON(raw, base)
		if err != nil {
			return base, err
		}
	}
	out.SchemaVersion = handoff.SchemaVersion
	out.Source = "llm_extract"
	out.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := out.Validate(); err != nil {
		return base, err
	}
	return out, nil
}

func (a *Agent) extractTaskStateLLMRaw(ctx context.Context, tapeName, prompt string, maxTokens int) (string, error) {
	return a.defaultChat.Chat(ctx, client.ChatOpts{
		Prompt:       prompt,
		SystemPrompt: handoff.TaskStateExtractSystemPrompt(),
		MaxTokens:    maxTokens,
		ContextLimit: a.handoffContextLimit(tapeName),
	})
}
