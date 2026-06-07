package tape

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
					"role":    "system",
					"content": "[Context Summary]\n" + content,
				})
			}
		case "content_replacement":
			// audit-only entry; not sent to LLM
			// anchor, event, error, exec_start, exec_input, exec_output,
			// exec_state, fork entries are not sent to LLM
		}
	}
	return messages
}
