package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/agent/toolresult"
	"github.com/seanly/dmr-devkit/client"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// runMode holds optional behavior for a single agent run (e.g. subagent delegation).
type runMode struct {
	tapeContextOverride *tape.TapeContext
	maxSteps            int // 0 = use Config.MaxSteps only; otherwise min(Config.MaxSteps, maxSteps)
	excludeToolNames    map[string]struct{}
	// When toolWhitelist is true, only allowedToolNames are visible (may be empty = no tools).
	// When false, allowedToolNames is ignored regardless of entries.
	toolWhitelist    bool
	allowedToolNames map[string]struct{}
	// Subagents is the allowlist of skill names this agent may delegate to.
	// Empty or nil means the agent cannot delegate to any subagent.
	subagents []string
	// promptParts carries multi-modal content (images) from the caller.
	// These are included in the tape entry and ChatOpts for the first LLM call.
	promptParts []provider.ContentPart
	// toolResultManager isolates large tool-output externalization state for a
	// subagent run. When nil, the agent's default manager is used.
	toolResultManager *toolresult.Manager
	// transient marks a child/subagent tape where persisted state should not be
	// restored or written (the child tape is discarded after the run).
	transient bool
}

// Run executes the agent loop: LLM call -> tool execution -> repeat.
func (a *Agent) Run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32) (*Result, error) {
	return a.RunWithOpts(ctx, tapeName, prompt, historyAfterEntryID, 0, "")
}

// RunWithContext executes the agent loop with optional plugin context.
// contextJSON is a JSON-encoded map[string]any that is passed to tool calls.
func (a *Agent) RunWithContext(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32, contextJSON string) (*Result, error) {
	return a.RunWithOpts(ctx, tapeName, prompt, historyAfterEntryID, 0, contextJSON)
}

// RunWithOpts executes the agent loop with optional max step limit and plugin context.
func (a *Agent) RunWithOpts(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32, maxSteps int, contextJSON string) (*Result, error) {
	return a.RunWithOptsAndTools(ctx, tapeName, prompt, historyAfterEntryID, maxSteps, nil, contextJSON)
}

// RunWithOptsAndTools executes the agent loop with max steps, optional tool whitelist, and plugin context.
// allowedTools: nil means no whitelist (all eligible tools remain visible). Non-nil restricts to names
// in *allowedTools; an empty pointed-to slice means no tools (text-only replies).
func (a *Agent) RunWithOptsAndTools(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32, maxSteps int, allowedTools *[]string, contextJSON string) (*Result, error) {
	var mode *runMode
	// Always create mode when contextJSON carries extra data (e.g. image parts)
	// or when there are runtime constraints (maxSteps, allowedTools).
	if maxSteps > 0 || allowedTools != nil || contextJSON != "" {
		mode = &runMode{maxSteps: maxSteps}
	}
	if allowedTools != nil {
		mode.toolWhitelist = true
		mode.allowedToolNames = make(map[string]struct{}, len(*allowedTools))
		for _, name := range *allowedTools {
			mode.allowedToolNames[name] = struct{}{}
		}
	}
	result, toolIterations, err := a.run(ctx, tapeName, prompt, historyAfterEntryID, mode, contextJSON)

	// Notify hooks that an agent run completed (used by webserver for SSE push)
	turn := a.countUserTurns(tapeName)
	_ = a.hooks.AfterAgentRun(ctx, AfterAgentRunArgs{
		TapeName:       tapeName,
		Turn:           turn,
		ToolIterations: toolIterations,
	})

	return result, err
}

func (a *Agent) run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32, mode *runMode, contextJSON string) (result *Result, toolIterations int, err error) {
	// --- OpenTelemetry span wrapper ---
	if a.config.Tracer != nil {
		var finish func(error)
		ctx, finish = a.config.Tracer.StartAgent(ctx, tapeName, "agent")
		defer finish(err)
	}
	// ------------------------------------

	// --- Execution lifecycle tracking ---
	execID := fmt.Sprintf("exec-%d", time.Now().UnixNano())
	agentID := ""
	if model := a.GetCurrentModel(tapeName); model != nil {
		agentID = model.Name
	}
	tc := tape.NewTapeController(a.tape)
	_ = tc.RecordExecStart(tapeName, execID, agentID, nil)
	_ = tc.RecordExecState(tapeName, execID, tape.ExecStatePending)
	if a.executor != nil {
		a.executor.MaxDuplicateToolCalls = a.config.MaxDuplicateToolCalls
		a.executor.ResetBudget(tapeName)
	}
	defer func() {
		if err != nil {
			_ = tc.RecordExecState(tapeName, execID, tape.ExecStateFailed)
		} else if result != nil {
			_ = tc.RecordExecState(tapeName, execID, tape.ExecStateCompleted)
		}
	}()
	// ------------------------------------

	// Restore persisted tape state for restart recovery (skip transient subagent tapes).
	if mode == nil || !mode.transient {
		a.restoreTapeState(tapeName)
	}

	// Auto-apply per-tape model from config (first time only; respects runtime ,model.switch).
	ts := a.tapeStates.get(tapeName)
	hasOverride := ts != nil && ts.modelOverride != ""
	if !hasOverride {
		if modelName := a.modelNameForTape(tapeName); modelName != "" {
			if switchErr := a.SwitchModel(tapeName, modelName); switchErr != nil {
				slog.Warn("tape model override failed", "model", modelName, "tape", tapeName, "error", switchErr)
			}
		}
	}

	toolIterations = 0

	// InterceptInput hook: let extensions handle commands before LLM
	ir, err := a.hooks.InterceptInput(ctx, InterceptInputArgs{
		TapeName:     tapeName,
		Prompt:       prompt,
		Workspace:    a.config.Workspace,
		TapeStore:    a.tape.Store,
		TapeManager:  a.tape,
		RuntimeAgent: a,
		TapeControl:  a.config.TapeControl,
		DefaultTape:  a.config.DefaultTape,
	})
	if err != nil {
		return nil, 0, core.FromError(err).
			Phase(core.PhaseIntercept).
			With("tape", tapeName).
			Build()
	}
	if ir != nil {
		if err := a.tape.AppendEntry(tapeName, tape.NewEventEntry("command", map[string]any{
			"raw":    prompt,
			"output": ir.Output,
			"status": "ok",
		})); err != nil {
			slog.Warn("tape append failed", "tape", tapeName, "error", err)
		}
		return &Result{Output: ir.Output, Steps: 0, SwitchTape: ir.SwitchTape, Quit: ir.Quit, ClearScreen: ir.ClearScreen}, 0, nil
	}

	// Collect tools from plugins (with discovery support)
	tools := a.collectToolsWithDiscovery(ctx, tapeName)
	tools = filterExcludedTools(tools, mode)

	toolCtx := tool.NewToolContext(ctx, tapeName, "")
	toolCtx.Workspace = a.config.Workspace
	toolCtx.State[tool.StateKeyRuntimeWorkspace] = a.config.Workspace // backward compat
	toolCtx.State[tool.StateKeyTapeStore] = a.tape.Store
	toolCtx.State[tool.StateKeyTapeManager] = a.tape
	toolCtx.State[tool.StateKeyRuntimeAgent] = a
	if mode != nil && len(mode.subagents) > 0 {
		toolCtx.State["subagent_allowlist"] = mode.subagents
	}

	// Parse and set plugin context if provided
	var pluginContext map[string]any
	if contextJSON != "" {
		if err := json.Unmarshal([]byte(contextJSON), &pluginContext); err == nil {
			toolCtx.Context = pluginContext
		} else {
			slog.Warn("agent: failed to parse plugin context JSON", "error", err)
		}
	}
	stepSystemOverride := systemPromptOverrideFromPluginContext(pluginContext)

	// Extract multi-modal prompt parts from plugin context (set by feishu/web etc.)
	// "_dmr_prompt_parts" is a JSON array of part objects (text/image_url).
	if mode != nil && pluginContext != nil {
		if raw, ok := pluginContext["_dmr_prompt_parts"]; ok {
			slog.Debug("agent: found _dmr_prompt_parts in pluginContext",
				"tape", tapeName, "count_hint", fmt.Sprintf("%T", raw))
			if rawArr, ok := raw.([]any); ok {
				for _, rp := range rawArr {
					if pm, ok := rp.(map[string]any); ok {
						cp := provider.ContentPartFromMap(pm)
						if cp != nil {
							mode.promptParts = append(mode.promptParts, cp)
						}
					}
				}
			}
		}
	}

	// Check if the current model supports vision; clear parts if not.
	if mode != nil && len(mode.promptParts) > 0 {
		model := a.GetCurrentModel(tapeName)
		if model != nil && !model.SupportsVision() {
			mode.promptParts = nil
			slog.Debug("agent: cleared image parts — model does not support vision",
				"model", model.Name, "tape", tapeName)
		}
	}

	currentPrompt := prompt
	consecutiveDenies := 0
	autoHandoffDone := false
	histAfter := int(historyAfterEntryID)

	// Record session anchor only if tape has no anchors yet (bootstrap)
	ts = a.tapeStates.getOrCreate(tapeName)
	ts.mu.Lock()
	alreadyStarted := ts.sessionStarted
	if !alreadyStarted {
		ts.sessionStarted = true
	}
	ts.mu.Unlock()
	if !alreadyStarted {
		anchors, _ := a.tape.Store.FetchAll(tapeName, &tape.FetchOpts{Kinds: []string{"anchor"}})
		if len(anchors) == 0 {
			a.Handoff(tapeName, "session/start", map[string]any{"owner": "human"})
		}
	}
	systemPrompt := mergeWorkflowStepSystemPrompt(a.resolveSystemPrompt(ctx, tapeName), stepSystemOverride)
	if err := a.appendSystemPromptEntry(tapeName, systemPrompt); err != nil {
		slog.Warn("tape append failed", "tape", tapeName, "error", err)
	}
	// Include multi-modal parts in the user message entry when provided
	userPayload := map[string]any{"role": "user", "content": prompt}
	if mode != nil && len(mode.promptParts) > 0 {
		parts := make([]any, 0, len(mode.promptParts)+1)
		parts = append(parts, map[string]any{"type": "text", "text": prompt})
		for _, p := range mode.promptParts {
			parts = append(parts, provider.ContentPartToMap(p))
		}
		userPayload["parts"] = parts
	}
	if err := a.tape.AppendEntry(tapeName, tape.NewMessageEntry(userPayload)); err != nil {
		slog.Warn("tape append failed", "tape", tapeName, "error", err)
	}
	a.initTaskStateFromPrompt(tapeName, prompt)

	lastPromptTokens := 0
	lastCompletionTokens := 0

	maxSteps := a.config.MaxSteps
	if mode != nil && mode.maxSteps > 0 && mode.maxSteps < maxSteps {
		maxSteps = mode.maxSteps
	}

	for step := 1; step <= maxSteps; step++ {
		systemPrompt = mergeWorkflowStepSystemPrompt(a.resolveSystemPrompt(ctx, tapeName), stepSystemOverride)

		// Re-collect tools each step to include newly discovered tools
		// Cache the tool list per tape for the duration of this step; invalidated on discovery.
		tools = a.collectToolsWithDiscoveryCached(ctx, tapeName)
		tools = filterExcludedTools(tools, mode)
		tools = filterAllowedTools(tools, mode)

		var tapeCtx *tape.TapeContext
		if histAfter <= 0 {
			if mode != nil && mode.tapeContextOverride != nil {
				tapeCtx = mode.tapeContextOverride
			} else {
				tapeCtx = a.tapeContextForTape(tapeName)
			}
		}
		tokensEst := 0
		if histAfter <= 0 {
			tokensEst = a.estimateContextTokens(tapeName, tapeCtx)
		}
		a.recordStepStart(tapeName, execID, step, tokensEst, len(tools))
		trManager := a.toolResults
		if mode != nil && mode.toolResultManager != nil {
			trManager = mode.toolResultManager
		}
		opts := client.ChatOpts{
			Prompt:              currentPrompt,
			SystemPrompt:        systemPrompt,
			Tools:               tools,
			ToolContext:         toolCtx,
			Tape:                tapeName,
			Context:             tapeCtx,
			HistoryAfterEntryID: histAfter,
			MaxTokens:           a.completionMaxTokensForTape(tapeName),
			ToolResultManager:   trManager,
			ContextLimit:        a.handoffContextLimit(tapeName),
		}
		if model := a.GetCurrentModel(tapeName); model != nil && !model.SupportsVision() {
			opts.StripImageParts = true
		}
		if mode != nil && len(mode.promptParts) > 0 {
			opts.PromptParts = mode.promptParts
		}

		// Get per-tape ChatClient
		chatClient := a.getChatClient(tapeName)

		// Pre-check: Estimate tokens before calling API to avoid wasting a call
		// if context is likely to overflow. This is a best-effort optimization.
		if histAfter <= 0 && a.preemptiveCompactEnabled() {
			estimatedTokens := a.estimateContextTokens(tapeName, tapeCtx)
			a.contextBudgetForTape(tapeName).UpdateEstimated(estimatedTokens)
			if estimatedTokens > 0 && a.shouldAutoHandoffByEstimate(tapeName, estimatedTokens) {
				slog.Info("compact: preemptive trigger", "estimated_tokens", estimatedTokens)
				limit := a.handoffContextLimit(tapeName)
				threshold := a.handoffThreshold(tapeName)
				if a.shouldCompactNow(tapeName, step, estimatedTokens, limit, threshold) {
					handoffName := fmt.Sprintf("auto:preemptive:%s", time.Now().UTC().Format("20060102-150405"))
					if ok, _ := a.performContextHandoff(ctx, tapeName, handoffName, "preemptive", step); ok || a.taskStateEnabled() {
						slog.Info("compact: preemptive handoff done", "anchor", handoffName)
						a.recordCompactStep(tapeName, step)

						// Rebuild system prompt and continue with structured prompt
						systemPrompt = mergeWorkflowStepSystemPrompt(a.resolveSystemPrompt(ctx, tapeName), stepSystemOverride)
						if err := a.appendSystemPromptEntry(tapeName, systemPrompt); err != nil {
							slog.Warn("tape append failed", "tape", tapeName, "error", err)
						}
						if err := a.tape.AppendEntry(tapeName, tape.NewMessageEntry(map[string]any{
							"role":    "user",
							"content": continueAfterCompactPrompt,
						})); err != nil {
							slog.Warn("tape append failed", "tape", tapeName, "error", err)
						}
						continue
					}
				} else {
					slog.Debug("compact: preemptive skipped (too soon or compact failed)")
				}
			}
		}

		// Check BeforeToolCall hooks will be done inside executor
		result, err := func() (*core.ToolAutoResult, error) {
			callCtx := ctx
			var finishLLM func(int, error)
			if a.config.Tracer != nil {
				modelName := "unknown"
				if m := a.GetCurrentModel(tapeName); m != nil {
					modelName = m.Model
				}
				callCtx, finishLLM = a.config.Tracer.StartLLMCall(ctx, "llm", modelName)
			}
			res, err := chatClient.RunTools(callCtx, opts)
			if finishLLM != nil {
				totalTokens := 0
				if res != nil && res.Usage != nil {
					totalTokens, _ = intFromUsageMap(res.Usage, "total_tokens")
				}
				finishLLM(totalTokens, err)
			}
			return res, err
		}()
		if err != nil {
			// Wrap with structured metadata so downstream recovery dispatch is uniform.
			err = core.FromError(err).
				Phase(core.PhaseLLMCall).
				With("step", step).
				With("tape", tapeName).
				Build()

			// Auto-compact on context overflow: compact and replay last round
			if !autoHandoffDone && isContextOverflowError(err) {
				handled, handoffErr := a.handleContextOverflow(ctx, tapeName, step, currentPrompt)
				if handoffErr != nil {
					return nil, toolIterations, handoffErr
				}
				if handled {
					autoHandoffDone = true
					continue
				}
			}

			// Log transient failures with structured context for observability.
			if se, ok := core.AsStructured(err); ok && se.IsRetryable() {
				slog.Warn("agent step transient failure",
					"step", step, "kind", se.Kind,
					"source", se.Source, "action", se.Action)
			}
			return nil, toolIterations, err
		}

		// Capture token usage from the latest successful LLM call (best effort).
		if result.Usage != nil {
			if pt, ok := intFromUsageMap(result.Usage, "prompt_tokens"); ok {
				lastPromptTokens = pt
			}
			if ct, ok := intFromUsageMap(result.Usage, "completion_tokens"); ok {
				lastCompletionTokens = ct
			}
		}

		switch result.Kind {
		case "text":
			assistantEntry := map[string]any{"role": "assistant", "content": result.Text}
			if result.Reasoning != "" {
				assistantEntry["reasoning"] = result.Reasoning
			}
			if err := a.tape.AppendEntry(tapeName, tape.NewMessageEntry(assistantEntry)); err != nil {
				slog.Warn("tape append failed", "tape", tapeName, "error", err)
			}
			trManager.NoteAssistantTurn(tapeName, time.Now())
			if err := a.tape.AppendEntry(tapeName, tape.NewEventEntry("run", map[string]any{"status": "ok"})); err != nil {
				slog.Warn("tape append failed", "tape", tapeName, "error", err)
			}
			a.recordRunEnd(tapeName, step, toolIterations, lastPromptTokens)
			return &Result{
				Output:           result.Text,
				Steps:            step,
				PromptTokens:     lastPromptTokens,
				CompletionTokens: lastCompletionTokens,
			}, toolIterations, nil

		case "tools":
			// Tools were executed, build continuation messages
			toolIterations++
			if result.Text != "" && len(result.ToolCalls) == 0 {
				return &Result{
					Output:           result.Text,
					Steps:            step,
					PromptTokens:     lastPromptTokens,
					CompletionTokens: lastCompletionTokens,
				}, toolIterations, nil
			}

			// Build tool result messages for next round
			var msgs []map[string]any

			// Add assistant message with tool calls
			if len(result.ToolCalls) > 0 {
				tcMaps := make([]map[string]any, 0, len(result.ToolCalls))
				for _, tc := range result.ToolCalls {
					tcMaps = append(tcMaps, map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Function.Name,
							"arguments": tc.Function.Arguments,
						},
					})
				}
				assistantWithTools := map[string]any{
					"role":       "assistant",
					"content":    result.Text,
					"tool_calls": tcMaps,
				}
				if result.Reasoning != "" {
					assistantWithTools["reasoning"] = result.Reasoning
				}
				msgs = append(msgs, assistantWithTools)

				toolBy := make(map[string]*tool.Tool, len(tools))
				for _, tt := range tools {
					if tt != nil {
						toolBy[tt.Spec.Name] = tt
					}
				}
				cfgChars := a.toolResultMaxCharsForTape(tapeName)

				// Add tool results
				for i, tr := range result.ToolResults {
					callID := ""
					toolName := ""
					toolArgs := ""
					if i < len(result.ToolCalls) {
						callID = result.ToolCalls[i].ID
						toolName = result.ToolCalls[i].Function.Name
						toolArgs = result.ToolCalls[i].Function.Arguments
					}
					th := cfgChars
					var tInst *tool.Tool
					if toolName != "" {
						tInst = toolBy[toolName]
						th = trManager.EffectiveThreshold(tInst, cfgChars, toolName)
					}
					content := trManager.ProcessNew(th, tapeName, callID, toolName, tr)
					msgs = append(msgs, map[string]any{
						"role":         "tool",
						"tool_call_id": callID,
						"content":      content,
					})

					// Notify callback
					a.onToolCallMu.RLock()
					fn := a.config.OnToolCall
					a.onToolCallMu.RUnlock()
					if fn != nil {
						fn(ToolCallEvent{
							Name:      toolName,
							Arguments: toolArgs,
							Result:    content,
						})
					}

					// Notify UI widget callback: any tool may embed validated A2UI (LLM sends it via send_a2ui_json_to_client; demos may ship fixed shells from custom tools too).
					if m, ok := tr.(map[string]any); ok {
						if widget, has := m["validated_a2ui_json"]; has {
							a.onToolCallMu.RLock()
							wf := a.config.OnUIWidget
							a.onToolCallMu.RUnlock()
							if wf != nil {
								wf(widget)
							}
						}
					}
				}
			}

			trManager.NoteAssistantTurn(tapeName, time.Now())
			for _, r := range trManager.ApplyTurnBudget(tapeName, msgs) {
				if err := a.tape.AppendEntry(tapeName, tape.NewContentReplacementEntry(r.ToolCallID, r.Replacement)); err != nil {
					slog.Warn("tape append content_replacement failed", "tape", tapeName, "error", err)
				}
			}

			// Record to tape
			a.tape.RecordChat(tape.RecordChatOpts{
				Tape:        tapeName,
				Messages:    msgs,
				ToolCalls:   result.ToolCalls,
				ToolResults: result.ToolResults,
			})

			toolNames := make([]string, 0, len(result.ToolCalls))
			denyCount := 0
			for i, tr := range result.ToolResults {
				if i < len(result.ToolCalls) {
					toolNames = append(toolNames, result.ToolCalls[i].Function.Name)
				}
				if m, ok := tr.(map[string]any); ok {
					if kind, _ := m["kind"].(string); kind == "denied" {
						denyCount++
					}
				}
			}
			a.recordToolRound(tapeName, step, toolNames, denyCount)
			a.updateTaskStateAfterToolRound(ctx, tapeName, step)
			review := a.runPostToolReview(ctx, tapeName, step, toolNames, result.ToolResults)
			if review.Feedback != "" {
				_ = a.tape.AppendEntry(tapeName, tape.NewSystemEntry(review.Feedback))
			}
			if review.HardStop {
				currentPrompt = "Address the review feedback above before taking further action. Do not repeat the blocked approach."
				if err := a.tape.AppendEntry(tapeName, tape.NewMessageEntry(map[string]any{
					"role": "user", "content": currentPrompt,
				})); err != nil {
					slog.Warn("tape append failed", "tape", tapeName, "error", err)
				}
				continue
			}

			// Check if all tool calls in this round were denied
			allDenied := len(result.ToolResults) > 0
			hasMeaningfulComment := false
			for _, tr := range result.ToolResults {
				if m, ok := tr.(map[string]any); ok {
					if kind, _ := m["kind"].(string); kind == "denied" {
						if msg, _ := m["message"].(string); msg != "" {
							if strings.Contains(msg, "denied by user") && len(msg) > len("denied by user: ") {
								hasMeaningfulComment = true
							}
						}
						continue
					}
				}
				allDenied = false
				break
			}
			if allDenied {
				if hasMeaningfulComment {
					// User provided feedback; give model more chances to adjust
					if consecutiveDenies >= 4 {
						msg := "all tool calls denied by policy despite user feedback, stopping"
						if err := a.tape.AppendEntry(tapeName, tape.NewEventEntry("run", map[string]any{"status": "denied"})); err != nil {
							slog.Warn("tape append failed", "tape", tapeName, "error", err)
						}
						return &Result{
							Output:           msg,
							Steps:            step,
							PromptTokens:     lastPromptTokens,
							CompletionTokens: lastCompletionTokens,
						}, toolIterations, nil
					}
					consecutiveDenies++
				} else {
					consecutiveDenies++
					if consecutiveDenies >= 2 {
						msg := "all tool calls denied by policy, stopping"
						if err := a.tape.AppendEntry(tapeName, tape.NewEventEntry("run", map[string]any{"status": "denied"})); err != nil {
							slog.Warn("tape append failed", "tape", tapeName, "error", err)
						}
						return &Result{
							Output:           msg,
							Steps:            step,
							PromptTokens:     lastPromptTokens,
							CompletionTokens: lastCompletionTokens,
						}, toolIterations, nil
					}
				}
			} else {
				consecutiveDenies = 0
			}

			// Build continuation prompt based on any user comment from denials
			var userComment string
			for _, tr := range result.ToolResults {
				if m, ok := tr.(map[string]any); ok {
					if kind, _ := m["kind"].(string); kind == "denied" {
						if msg, _ := m["message"].(string); msg != "" {
							prefixes := []string{"denied by user: ", "denied by user (not selected): "}
							for _, prefix := range prefixes {
								if idx := strings.Index(msg, prefix); idx >= 0 {
									if comment := strings.TrimSpace(msg[idx+len(prefix):]); comment != "" {
										userComment = comment
										break
									}
								}
							}
						}
					}
				}
				if userComment != "" {
					break
				}
			}
			if userComment != "" {
				currentPrompt = fmt.Sprintf(
					"The user denied your previous tool request with the following feedback: %q. "+
						"You MUST adjust your strategy based on this feedback. Do not repeat the same request or use the same approach.",
					userComment,
				)
			} else if hasShellFailure(result.ToolCalls, result.ToolResults) {
				currentPrompt = "The previous shell command failed with a non-zero exit code. You MUST analyze the error output, identify what was wrong with your command, and fix it before proceeding. Do not assume the command succeeded."
			} else {
				currentPrompt = "Continue based on the tool results above."
			}

			// Proactive auto-handoff: check token usage after tool execution
			if result.Usage != nil && a.shouldAutoHandoff(tapeName, result.Usage) {
				pt, _ := intFromUsageMap(result.Usage, "prompt_tokens")
				a.contextBudgetForTape(tapeName).UpdateReported(pt)
				limit := a.handoffContextLimit(tapeName)
				threshold := a.handoffThreshold(tapeName)

				if !a.shouldCompactNow(tapeName, step, pt, limit, threshold) {
					slog.Warn("compact: skipped (too soon after last compact)", "current_step", step)
				} else {
					slog.Info("compact: triggered", "prompt_tokens", pt, "limit", limit, "threshold", threshold, "effective_limit", int(float64(limit)*threshold))

					handoffName := fmt.Sprintf("auto:token-threshold:%s", time.Now().UTC().Format("20060102-150405"))
					if ok, _ := a.performContextHandoff(ctx, tapeName, handoffName, "proactive", step); ok || a.taskStateEnabled() {
						slog.Info("compact: proactive handoff done", "anchor", handoffName)
					} else {
						slog.Error("compact: proactive handoff failed")
					}

					a.recordCompactStep(tapeName, step)

					currentPrompt = continueAfterCompactPrompt

					systemPrompt = mergeWorkflowStepSystemPrompt(a.resolveSystemPrompt(ctx, tapeName), stepSystemOverride)
					if err := a.appendSystemPromptEntry(tapeName, systemPrompt); err != nil {
						slog.Warn("tape append failed", "tape", tapeName, "error", err)
					}
					if err := a.tape.AppendEntry(tapeName, tape.NewMessageEntry(map[string]any{
						"role": "user", "content": currentPrompt,
					})); err != nil {
						slog.Warn("tape append failed", "tape", tapeName, "error", err)
					}
					continue
				}
			}

		case "error":
			if result.Error != nil {
				return nil, toolIterations, core.New(core.ErrKind(result.Error.Kind), fmt.Sprintf("step %d: %s", step, result.Error.Message)).
					Phase(core.PhaseLLMCall).With("step", step).Build()
			}
			return nil, toolIterations, core.New(core.ErrKindUnknown, fmt.Sprintf("step %d: unknown error", step)).
				Phase(core.PhaseLLMCall).With("step", step).Build()

		default:
			return nil, toolIterations, core.New(core.ErrKindUnknown, fmt.Sprintf("step %d: unexpected result kind: %s", step, result.Kind)).
				Phase(core.PhaseLLMCall).With("step", step).With("kind", result.Kind).Build()
		}
	}

	return nil, toolIterations, core.New(core.ErrKindUnknown, fmt.Sprintf("max steps reached (%d)", maxSteps)).
		Phase(core.PhaseLLMCall).With("max_steps", maxSteps).Build()
}
