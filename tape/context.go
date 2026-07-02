package tape

import (
	"fmt"
	"strings"

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
	// KeepTaskState controls whether task_state entries are injected.
	KeepTaskState bool
	// SkipPoorSummaries suppresses compact_summary entries whose quality is "poor".
	// This is used when quality fallback is enabled: a bad summary is dropped and
	// the model relies on task_state + recent raw messages instead.
	SkipPoorSummaries bool
	// Strategy selects how tape entries are transformed into LLM messages.
	Strategy config.CompactStrategy
	// builder is attached by TapeManager.ReadMessages so that standalone
	// TapeContext.BuildMessages can delegate to the builder pipeline.
	builder *ContextBuilder
}

// NewLastAnchorContext creates a TapeContext that windows from the last anchor.
func NewLastAnchorContext() *TapeContext {
	return &TapeContext{AnchorMode: LastAnchorS, KeepSummary: true, KeepTaskState: true}
}

// NewNamedAnchorContext creates a TapeContext that windows from a named anchor.
func NewNamedAnchorContext(name string) *TapeContext {
	return &TapeContext{AnchorMode: NamedAnchor, AnchorName: name, KeepSummary: true, KeepTaskState: true}
}

// NewNoAnchorContext creates a TapeContext with no anchor filtering.
func NewNoAnchorContext() *TapeContext {
	return &TapeContext{AnchorMode: NoAnchor, KeepSummary: true, KeepTaskState: true}
}

// NewSoftBoundaryContext creates a TapeContext that keeps KeepBefore raw messages
// before the last anchor in addition to the anchor-to-end window.
func NewSoftBoundaryContext(keepBefore int) *TapeContext {
	if keepBefore < 0 {
		keepBefore = 0
	}
	return &TapeContext{
		AnchorMode:    LastAnchorS,
		SoftBoundary:  true,
		KeepBefore:    keepBefore,
		KeepSummary:   true,
		KeepTaskState: true,
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
	return applyCompactStrategy(buildMessages(entries, tc), tc.Strategy)
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

// formatTaskStateBlock renders task_state payload for LLM injection.
func formatTaskStateBlock(payload map[string]any) (string, bool) {
	content, ok := payload["goal"].(string)
	if !ok || content == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString("goal: ")
	b.WriteString(content)
	b.WriteByte('\n')
	if constraints, ok := payload["constraints"].(map[string]any); ok && len(constraints) > 0 {
		b.WriteString("constraints:\n")
		for k, v := range constraints {
			b.WriteString("  ")
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(fmt.Sprint(v))
			b.WriteByte('\n')
		}
	}
	if pending, ok := payload["pending"].([]any); ok && len(pending) > 0 {
		b.WriteString("pending:\n")
		for _, item := range pending {
			if m, ok := item.(map[string]any); ok {
				if summary, ok := m["summary"].(string); ok && summary != "" {
					b.WriteString("  - ")
					b.WriteString(summary)
					b.WriteByte('\n')
				}
			}
		}
	}
	if completed, ok := payload["completed"].([]any); ok && len(completed) > 0 {
		b.WriteString("completed:\n")
		for _, item := range completed {
			if m, ok := item.(map[string]any); ok {
				if summary, ok := m["summary"].(string); ok && summary != "" {
					b.WriteString("  - ")
					b.WriteString(summary)
					b.WriteByte('\n')
				}
			}
		}
	}
	if la, ok := payload["last_action"].(string); ok && la != "" {
		b.WriteString("last_action: ")
		b.WriteString(la)
		b.WriteByte('\n')
	}
	if af, ok := payload["active_files"].([]any); ok && len(af) > 0 {
		b.WriteString("active_files: ")
		for i, x := range af {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprint(x))
		}
		b.WriteByte('\n')
	}
	if artifacts, ok := payload["artifacts"].([]any); ok && len(artifacts) > 0 {
		b.WriteString("artifacts:\n")
		for _, item := range artifacts {
			if m, ok := item.(map[string]any); ok {
				typ, _ := m["type"].(string)
				ref, _ := m["ref"].(string)
				label, _ := m["label"].(string)
				if ref == "" {
					continue
				}
				b.WriteString("  - ")
				if label != "" {
					b.WriteString(label)
					b.WriteString(" (")
					b.WriteString(ref)
					b.WriteString(")")
				} else {
					b.WriteString(ref)
				}
				if typ != "" {
					b.WriteString(" [")
					b.WriteString(typ)
					b.WriteString("]")
				}
				b.WriteByte('\n')
			}
		}
	}
	return b.String(), true
}
