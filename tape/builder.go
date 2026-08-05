package tape

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ContextBuilder turns audit tape entries into LLM API messages.
// It is read-only: it never mutates the underlying tape.
type ContextBuilder struct {
	Store TapeStore
}

// NewContextBuilder creates a builder backed by the given store.
func NewContextBuilder(store TapeStore) *ContextBuilder {
	return &ContextBuilder{Store: store}
}

// ReadMessages fetches entries for the given tape according to ctx and builds
// the final message list, including any soft-boundary messages and strategy
// transformations.
func (b *ContextBuilder) ReadMessages(tape string, ctx *TapeContext) ([]map[string]any, error) {
	if ctx == nil {
		ctx = NewLastAnchorContext()
	}

	entries, err := b.FetchEntries(tape, ctx)
	if err != nil {
		return nil, err
	}

	messages := b.BuildMessages(entries, ctx)

	// Soft boundary: retain the last KeepBefore raw messages before the anchor.
	if ctx.SoftBoundary && ctx.KeepBefore > 0 && ctx.AnchorMode != NoAnchor {
		before, err := b.fetchBeforeAnchor(tape, ctx.AnchorName, ctx.KeepBefore)
		if err == nil && len(before) > 0 {
			beforeCtx := &TapeContext{
				AnchorMode:    NoAnchor,
				KeepSummary:   false,
				Strategy:      ctx.Strategy,
			}
			beforeMsgs := b.BuildMessages(before, beforeCtx)
			messages = append(messages, beforeMsgs...)
		}
	}

	return messages, nil
}

// BuildMessages converts already-fetched entries into LLM messages, applying
// the strategy transformation configured on ctx.
func (b *ContextBuilder) BuildMessages(entries []TapeEntry, ctx *TapeContext) []map[string]any {
	if ctx == nil {
		ctx = NewLastAnchorContext()
	}
	messages := buildMessages(entries, ctx)
	if ctx.Select != nil {
		// Custom selector replaces the default pipeline entirely.
		return messages
	}
	messages = applyProgressiveTrim(messages, ctx)
	return applyCompactStrategy(messages, ctx.Strategy)
}

// FetchEntries resolves FetchOpts from ctx.AnchorMode/AnchorName and returns
// the matching entries.
func (b *ContextBuilder) FetchEntries(tape string, ctx *TapeContext) ([]TapeEntry, error) {
	if b.Store == nil {
		return nil, fmt.Errorf("context builder has no store")
	}
	if ctx == nil {
		ctx = NewLastAnchorContext()
	}

	opts := &FetchOpts{}
	switch ctx.AnchorMode {
	case LastAnchorS:
		opts.LastAnchor = true
	case NamedAnchor:
		opts.AfterAnchor = ctx.AnchorName
	case NoAnchor:
		// no anchor filtering
	}

	return b.Store.FetchAll(tape, opts)
}

// fetchBeforeAnchor returns up to n raw message/system/tool entries immediately
// before the selected anchor. If anchorName is empty, the last anchor is used.
func (b *ContextBuilder) fetchBeforeAnchor(tape, anchorName string, n int) ([]TapeEntry, error) {
	if b.Store == nil {
		return nil, fmt.Errorf("context builder has no store")
	}
	all, err := b.Store.FetchAll(tape, nil)
	if err != nil {
		return nil, err
	}

	anchorIdx := -1
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Kind != "anchor" {
			continue
		}
		if anchorName == "" {
			anchorIdx = i
			break
		}
		if name, _ := all[i].Payload["name"].(string); name == anchorName {
			anchorIdx = i
			break
		}
	}
	if anchorIdx < 0 {
		return nil, nil
	}

	// If the anchor carries a quality-fallback hint, expand the raw-message window.
	keep := n
	if state, ok := all[anchorIdx].Payload["state"].(map[string]any); ok {
		keep = max(keep, intFromAny(state["fallback_keep_before"]))
	}

	start := anchorIdx - keep
	if start < 0 {
		start = 0
	}
	count := anchorIdx - start
	if count <= 0 {
		return nil, nil
	}

	out := make([]TapeEntry, 0, count)
	for i := start; i < anchorIdx; i++ {
		switch all[i].Kind {
		case "message", "system", "system_prompt", "tool_call", "tool_result", "compact_summary":
			out = append(out, all[i])
		}
	}
	return out, nil
}

// JoinedContent returns the textual content of a message map, handling simple
// strings and string arrays.
func JoinedContent(msg map[string]any) string {
	content := msg["content"]
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, p := range v {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "")
	case []string:
		return strings.Join(v, "")
	default:
		return ""
	}
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// applyProgressiveTrim applies the L1 graduated trimming and L3 microcompact
// passes configured on ctx. Both are read-time transformations: they never
// mutate the tape store, only the messages handed to the LLM.
func applyProgressiveTrim(messages []map[string]any, ctx *TapeContext) []map[string]any {
	if ctx == nil || len(messages) == 0 {
		return messages
	}
	if ctx.GraduatedTrim {
		messages = graduatedTrimToolResults(messages, ctx)
	}
	if ctx.ContextMicrocompact {
		messages = microcompactOldToolResults(messages, ctx)
	}
	if ctx.SnipDropUnits > 0 {
		messages = snipFrontUnits(messages, ctx.SnipDropUnits, ctx.SnipKeepRecentTurns)
	}
	return messages
}

// graduatedTrimToolResults truncates older tool_result content to a fraction of
// the configured budget while leaving the most recent tool messages intact. The
// kept portion preserves the head and tail of the original content with a
// marker in between so file paths and trailing status lines survive.
func graduatedTrimToolResults(messages []map[string]any, ctx *TapeContext) []map[string]any {
	maxChars := ctx.ToolResultMaxChars
	if maxChars <= 0 {
		return messages
	}
	recent := ctx.RecentToolResults
	if recent <= 0 {
		recent = 3
	}
	ratio := ctx.OldToolResultRatio
	if ratio <= 0 {
		ratio = 0.25
	}
	if ratio >= 1 {
		return messages
	}
	budget := int(float64(maxChars) * ratio)
	if budget <= 0 {
		budget = 1
	}

	// Collect indices of tool messages in order.
	toolIdx := make([]int, 0, 8)
	for i, m := range messages {
		if role, _ := m["role"].(string); role == "tool" {
			toolIdx = append(toolIdx, i)
		}
	}
	if len(toolIdx) <= recent {
		return messages
	}
	oldCount := len(toolIdx) - recent
	oldSet := make(map[int]bool, oldCount)
	for _, idx := range toolIdx[:oldCount] {
		oldSet[idx] = true
	}

	out := make([]map[string]any, len(messages))
	for i, m := range messages {
		if oldSet[i] {
			out[i] = graduatedTrimToolMessage(m, budget)
		} else {
			out[i] = m
		}
	}
	return out
}

// graduatedTrimToolMessage truncates a single tool message's string content to
// budget runes, keeping 75% of the budget from the head and 25% from the tail
// with a marker describing how much was elided.
func graduatedTrimToolMessage(msg map[string]any, budget int) map[string]any {
	content, ok := msg["content"].(string)
	if !ok {
		return msg
	}
	runes := []rune(content)
	if len(runes) <= budget {
		return msg
	}
	headBudget := int(float64(budget) * 0.75)
	if headBudget < 1 {
		headBudget = 1
	}
	if headBudget > len(runes) {
		headBudget = len(runes)
	}
	tailBudget := budget - headBudget
	if tailBudget < 0 {
		tailBudget = 0
	}
	if tailBudget > len(runes)-headBudget {
		tailBudget = len(runes) - headBudget
	}
	head := string(runes[:headBudget])
	tail := ""
	if tailBudget > 0 {
		tail = string(runes[len(runes)-tailBudget:])
	}
	elided := len(runes) - headBudget - tailBudget
	trimmed := head + fmt.Sprintf("\n[... truncated %d chars (old result) ...]\n", elided) + tail
	cp := shallowCopyMessage(msg)
	cp["content"] = trimmed
	return cp
}

// microcompactOldToolResults clears the content of tool_result messages that
// belong to completed earlier assistant tool-call rounds, leaving only the most
// recent round's results intact. The message structure (role/tool_call_id) is
// preserved so tool-call pairing stays valid for OpenAI-compatible APIs.
func microcompactOldToolResults(messages []map[string]any, ctx *TapeContext) []map[string]any {
	// Locate the index of the most recent assistant message carrying tool_calls;
	// every tool message before it is considered "old".
	lastAssistantWithTools := -1
	for i := len(messages) - 1; i >= 0; i-- {
		role, _ := messages[i]["role"].(string)
		if role == "assistant" {
			if _, ok := messages[i]["tool_calls"]; ok {
				lastAssistantWithTools = i
			}
			break
		}
	}
	if lastAssistantWithTools < 0 {
		return messages
	}
	out := make([]map[string]any, len(messages))
	changed := false
	for i, m := range messages {
		role, _ := m["role"].(string)
		if role == "tool" && i < lastAssistantWithTools {
			content, _ := m["content"].(string)
			if strings.TrimSpace(content) != "" {
				cp := shallowCopyMessage(m)
				cp["content"] = "[Old tool result content cleared]"
				out[i] = cp
				changed = true
				continue
			}
		}
		out[i] = m
	}
	if !changed {
		return messages
	}
	return out
}

// snipFrontUnits drops up to maxUnits safe units from the front of the message
// list. A safe unit is either:
//   - a standalone user or assistant-text message (no tool_calls), or
//   - an assistant message carrying tool_calls plus all immediately following
//     tool messages.
//
// System messages, compact_summary messages, and the last keepRecentTurns
// turns are protected and never dropped. snipFrontUnits never leaves a dangling
// tool message (one whose tool_call_id has no preceding assistant tool_calls).
func snipFrontUnits(messages []map[string]any, maxUnits, keepRecentTurns int) []map[string]any {
	if maxUnits <= 0 || len(messages) == 0 {
		return messages
	}
	if keepRecentTurns <= 0 {
		keepRecentTurns = 2
	}

	// Mark protected tail: find the start index of the last keepRecentTurns
	// turns. A "turn" boundary starts at an assistant message (with or without
	// tool_calls) or a user message.
	protectedStart := len(messages)
	turnsSeen := 0
	for i := len(messages) - 1; i >= 0; i-- {
		role, _ := messages[i]["role"].(string)
		if role == "assistant" || role == "user" {
			turnsSeen++
			protectedStart = i
			if turnsSeen >= keepRecentTurns {
				break
			}
		}
	}

	dropped := 0
	out := make([]map[string]any, 0, len(messages))
	i := 0
	for i < len(messages) {
		if dropped >= maxUnits || i >= protectedStart {
			out = append(out, messages[i])
			i++
			continue
		}
		role, _ := messages[i]["role"].(string)
		// Never snip system / compact_summary messages: keep them in place.
		if role == "system" {
			out = append(out, messages[i])
			i++
			continue
		}
		if role == "assistant" {
			if _, hasCalls := messages[i]["tool_calls"]; hasCalls {
				// Drop the assistant message and its trailing tool messages.
				i++
				for i < len(messages) {
					r, _ := messages[i]["role"].(string)
					if r != "tool" {
						break
					}
					if i >= protectedStart {
						break
					}
					i++
				}
				dropped++
				continue
			}
			// Assistant text-only message: drop it.
			i++
			dropped++
			continue
		}
		if role == "user" {
			i++
			dropped++
			continue
		}
		// tool message at the front with no preceding assistant (dangling):
		// keep it rather than risk breaking pairing.
		out = append(out, messages[i])
		i++
	}
	return out
}

func buildMessages(entries []TapeEntry, ctx *TapeContext) []map[string]any {
	if ctx == nil {
		ctx = &TapeContext{KeepSummary: true}
	}

	// Pre-pass: collect exec_ids whose run was interrupted.
	interruptedExecs, _ := collectInterruptedExecs(entries)

	var messages []map[string]any
	execStartIdx := 0
	currentExecID := ""
	for _, e := range entries {
		switch e.Kind {
		case "exec_start":
			execStartIdx = len(messages)
			currentExecID, _ = e.Payload["exec_id"].(string)

		case "message":
			msg := make(map[string]any, len(e.Payload))
			for k, v := range e.Payload {
				msg[k] = v
			}
			messages = append(messages, msg)

		case "system":
			if content, ok := e.Payload["content"].(string); ok {
				messages = append(messages, map[string]any{"role": "system", "content": content})
			}

		case "system_prompt":
			// Runtime system prompts are audit-only; the agent loop injects the
			// current composed system prompt via ChatOpts.SystemPrompt on every turn.

		case "compact_summary":
			if !ctx.KeepSummary {
				continue
			}
			if summary, ok := ExtractCompactSummary(e.Payload); ok {
				if ctx.SkipPoorSummaries {
					if q, _ := e.Payload["quality"].(string); q == "poor" {
						continue
					}
				}
				messages = append(messages, map[string]any{
					"role":         "system",
					"content":      summary.Content,
					"context_kind": "compact_summary",
				})
			}

		case "event":
			name, _ := e.Payload["name"].(string)
			if name == EventRunInterrupted {
				data, _ := e.Payload["data"].(map[string]any)
				execID := currentExecID
				if data != nil {
					if id, _ := data["exec_id"].(string); id != "" {
						execID = id
					}
				}
				if execStartIdx <= len(messages) {
					execSlice := messages[execStartIdx:]
					messages = append(messages[:execStartIdx], trimIncompleteInterruptTail(execSlice)...)
				}
				meta, interrupted := interruptedExecs[execID]
				if interrupted && !meta.suppressNotice {
					messages = append(messages, map[string]any{
						"role":    "system",
						"content": InterruptedSystemNotice,
					})
				}
				currentExecID = ""
			}
			// other events: audit-only

		case "handoff_packet", "content_replacement":
			// handoff_packet audit-only
			// anchor, event, error, exec_*, fork entries are not sent to LLM
		}
	}
	return messages
}
