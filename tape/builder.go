package tape

import (
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
				KeepTaskState: false,
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

	start := anchorIdx - n
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

func buildMessages(entries []TapeEntry, ctx *TapeContext) []map[string]any {
	if ctx == nil {
		ctx = &TapeContext{KeepSummary: true, KeepTaskState: true}
	}
	var messages []map[string]any
	var taskStateBlock string
	if ctx.KeepTaskState {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].Kind == "task_state" {
				if block, ok := formatTaskStateBlock(entries[i].Payload); ok {
					taskStateBlock = block
				}
				break
			}
		}
	}
	if taskStateBlock != "" {
		messages = append(messages, map[string]any{
			"role":         "system",
			"content":      taskStateBlock,
			"context_kind": "task_state",
		})
	}
	for _, e := range entries {
		switch e.Kind {
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
		case "task_state", "handoff_packet", "content_replacement":
			// task_state injected above (latest only); handoff_packet audit-only
			// anchor, event, error, exec_* , fork entries are not sent to LLM
		}
	}
	return messages
}
