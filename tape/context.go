package tape

import (
	"github.com/seanly/dmr-devkit/config"
)

// AnchorSelector specifies which anchor to use for context windowing.
type AnchorSelector int

const (
	NoAnchor    AnchorSelector = iota // no anchor filtering
	LastAnchorS                       // use last anchor
	NamedAnchor                       // use a named anchor
)

// TapeContext controls how tape entries are windowed and converted to messages.
type TapeContext struct {
	AnchorMode AnchorSelector
	AnchorName string // only used when AnchorMode == NamedAnchor
	Select     func([]TapeEntry, *TapeContext) []map[string]any
	State      map[string]any

	// SoftBoundary keeps KeepBefore raw messages from immediately before the
	// selected anchor as an extra safety net against summary quality issues.
	SoftBoundary bool
	KeepBefore   int
	// KeepSummary controls whether compact_summary entries are injected.
	KeepSummary bool
	// SkipPoorSummaries suppresses compact_summary entries whose quality is "poor".
	// This is used when quality fallback is enabled: a bad summary is dropped and
	// the model relies on recent raw messages instead.
	SkipPoorSummaries bool
	// Strategy selects how tape entries are transformed into LLM messages.
	Strategy config.CompactStrategy
	// GraduatedTrim enables more aggressive truncation of older tool_result
	// content while keeping the most recent ones intact (L1 graduated trimming).
	GraduatedTrim bool
	// RecentToolResults is the count of most-recent tool messages kept intact.
	// 0 = 3.
	RecentToolResults int
	// OldToolResultRatio is the fraction of ToolResultMaxChars applied to older
	// tool messages (0..1). 0 = 0.25.
	OldToolResultRatio float64
	// ToolResultMaxChars is the per-result truncation budget (in runes) used as
	// the baseline for graduated trimming. 0 = graduated trimming disabled.
	ToolResultMaxChars int
	// ContextMicrocompact clears the content of older tool_result messages
	// (kept as structural placeholders) to reduce cold-prefix tokens (L3).
	ContextMicrocompact bool
	// SnipDropUnits drops up to this many safe "units" from the front of the
	// built message list to shed tokens without an LLM compact (L2 history
	// snip). A unit is a standalone user/assistant-text message, or an
	// assistant-with-tool_calls message plus its immediately following tool
	// messages. Protected messages (system, compact_summary, and the last
	// SnipKeepRecentTurns turns) are never dropped.
	SnipDropUnits int
	// SnipKeepRecentTurns is the number of most-recent turns protected from
	// snipping. 0 = 2.
	SnipKeepRecentTurns int
	// builder is attached by TapeManager.ReadMessages so that standalone
	// TapeContext.BuildMessages can delegate to the builder pipeline.
	builder *ContextBuilder
}

// NewLastAnchorContext creates a TapeContext that windows from the last anchor.
func NewLastAnchorContext() *TapeContext {
	return &TapeContext{AnchorMode: LastAnchorS, KeepSummary: true}
}

// NewNamedAnchorContext creates a TapeContext that windows from a named anchor.
func NewNamedAnchorContext(name string) *TapeContext {
	return &TapeContext{AnchorMode: NamedAnchor, AnchorName: name, KeepSummary: true}
}

// NewNoAnchorContext creates a TapeContext with no anchor filtering.
func NewNoAnchorContext() *TapeContext {
	return &TapeContext{AnchorMode: NoAnchor, KeepSummary: true}
}

// NewSoftBoundaryContext creates a TapeContext that keeps KeepBefore raw messages
// before the last anchor in addition to the anchor-to-end window.
func NewSoftBoundaryContext(keepBefore int) *TapeContext {
	if keepBefore < 0 {
		keepBefore = 0
	}
	return &TapeContext{
		AnchorMode:   LastAnchorS,
		SoftBoundary: true,
		KeepBefore:   keepBefore,
		KeepSummary:  true,
	}
}

// BuildMessages converts tape entries to message dicts suitable for LLM input.
// If the context has a custom Select function, that function is used; otherwise
// the default builder pipeline is used and the configured compact strategy is
// applied.
func (tc *TapeContext) BuildMessages(entries []TapeEntry) []map[string]any {
	if tc == nil {
		tc = NewLastAnchorContext()
	}
	if tc.Select != nil {
		return tc.Select(entries, tc)
	}
	if tc.builder != nil {
		return tc.builder.BuildMessages(entries, tc)
	}
	messages := buildMessages(entries, tc)
	messages = applyProgressiveTrim(messages, tc)
	return applyCompactStrategy(messages, tc.Strategy)
}

// SetBuilder attaches a ContextBuilder to this context so that BuildMessages can
// delegate to it. Used internally by TapeManager.ReadMessages.
func (tc *TapeContext) SetBuilder(b *ContextBuilder) {
	if tc == nil {
		return
	}
	tc.builder = b
}

func defaultBuildMessages(entries []TapeEntry, ctx *TapeContext) []map[string]any {
	if ctx == nil {
		ctx = &TapeContext{KeepSummary: true}
	}
	var messages []map[string]any
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
		case "handoff_packet", "content_replacement":
			// handoff_packet audit-only
			// anchor, event, error, exec_*, fork entries are not sent to LLM
		}
	}
	return messages
}
