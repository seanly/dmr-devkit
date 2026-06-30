package tape

import (
	"fmt"
	"strings"
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
}

// NewLastAnchorContext creates a TapeContext that windows from the last anchor.
func NewLastAnchorContext() *TapeContext {
	return &TapeContext{AnchorMode: LastAnchorS}
}

// NewNamedAnchorContext creates a TapeContext that windows from a named anchor.
func NewNamedAnchorContext(name string) *TapeContext {
	return &TapeContext{AnchorMode: NamedAnchor, AnchorName: name}
}

// NewNoAnchorContext creates a TapeContext with no anchor filtering.
func NewNoAnchorContext() *TapeContext {
	return &TapeContext{AnchorMode: NoAnchor}
}

// BuildMessages converts tape entries to message dicts suitable for LLM input.
func (tc *TapeContext) BuildMessages(entries []TapeEntry) []map[string]any {
	if tc.Select != nil {
		return tc.Select(entries, tc)
	}
	return defaultBuildMessages(entries)
}

func defaultBuildMessages(entries []TapeEntry) []map[string]any {
	var messages []map[string]any
	var taskStateBlock string
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "task_state" {
			if block, ok := formatTaskStateBlock(entries[i].Payload); ok {
				taskStateBlock = block
			}
			break
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
		case "compact_summary":
			if content, ok := e.Payload["content"].(string); ok {
				messages = append(messages, map[string]any{
					"role":         "system",
					"content":      content,
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
