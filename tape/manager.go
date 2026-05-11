package tape

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/seanly/dmr-devkit/core"
)

// TapeManager provides high-level tape operations.
type TapeManager struct {
	Store TapeStore
}

func NewTapeManager(store TapeStore) *TapeManager {
	return &TapeManager{Store: store}
}

// ReadMessages reads tape entries and converts them to messages using the context.
func (m *TapeManager) ReadMessages(tape string, ctx *TapeContext) ([]map[string]any, error) {
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

	entries, err := m.Store.FetchAll(tape, opts)
	if err != nil {
		return nil, err
	}

	return ctx.BuildMessages(entries), nil
}

// AppendEntry appends a single entry to a tape.
func (m *TapeManager) AppendEntry(tape string, entry TapeEntry) error {
	return m.Store.Append(tape, entry)
}

// Handoff creates an anchor entry and an event entry, returning the created entries.
func (m *TapeManager) Handoff(tape, name string, state map[string]any) ([]TapeEntry, error) {
	anchor := NewAnchorEntry(name, state)
	if err := m.Store.Append(tape, anchor); err != nil {
		return nil, fmt.Errorf("append anchor: %w", err)
	}

	eventData := map[string]any{"name": name}
	if state != nil {
		eventData["state"] = state
	}
	event := NewEventEntry("handoff", eventData)
	if err := m.Store.Append(tape, event); err != nil {
		return nil, fmt.Errorf("append handoff event: %w", err)
	}

	return []TapeEntry{anchor, event}, nil
}

// RecordChatOpts holds options for recording a chat exchange.
type RecordChatOpts struct {
	Tape         string
	SystemPrompt string
	Messages     []map[string]any
	ToolCalls    []core.ToolCallData
	ToolResults  []any
	Error        *core.ErrorPayload
	Usage        map[string]any
}

// RecordChat records a complete chat exchange as tape entries.
// Errors during individual appends are logged but do not stop recording.
func (m *TapeManager) RecordChat(opts RecordChatOpts) {
	if opts.SystemPrompt != "" {
		if err := m.Store.Append(opts.Tape, NewSystemEntry(opts.SystemPrompt)); err != nil {
			slog.Warn("tape append system entry failed", "error", err)
		}
	}

	for _, msg := range opts.Messages {
		if err := m.Store.Append(opts.Tape, NewMessageEntry(msg)); err != nil {
			slog.Warn("tape append message failed", "error", err)
		}
	}

	// Record tool calls as a single entry (bub format: calls array)
	if len(opts.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(opts.ToolCalls))
		for _, call := range opts.ToolCalls {
			calls = append(calls, map[string]any{
				"id":       call.ID,
				"type":     "function",
				"function": map[string]any{"name": call.Function.Name, "arguments": call.Function.Arguments},
			})
		}
		if err := m.Store.Append(opts.Tape, NewToolCallEntry(calls)); err != nil {
			slog.Warn("tape append tool calls failed", "error", err)
		}
	}

	// Record tool results as a single entry (bub format: results array)
	if len(opts.ToolResults) > 0 {
		results := make([]any, len(opts.ToolResults))
		copy(results, opts.ToolResults)
		if err := m.Store.Append(opts.Tape, NewToolResultEntry(results)); err != nil {
			slog.Warn("tape append tool results failed", "error", err)
		}
	}

	if opts.Error != nil {
		if err := m.Store.Append(opts.Tape, NewErrorEntry(opts.Error.Kind, opts.Error.Message)); err != nil {
			slog.Warn("tape append error entry failed", "error", err)
		}
	}

	status := "ok"
	if opts.Error != nil {
		status = "error"
	}
	eventData := map[string]any{"status": status}
	if opts.Usage != nil {
		eventData["usage"] = opts.Usage
	}
	if err := m.Store.Append(opts.Tape, NewEventEntry("run", eventData)); err != nil {
		slog.Warn("tape append run event failed", "error", err)
	}
}

// CompactOpts holds options for compacting a tape.
type CompactOpts struct {
	Tape       string
	AnchorName string // optional, defaults to compact:<timestamp>
	Summarizer func(ctx context.Context, messages []map[string]any) (string, error)
}

// Compact generates a summary of the current tape context and creates a compact anchor.
// Returns the created entries: [anchor, compact_summary, event].
func (m *TapeManager) Compact(ctx context.Context, opts CompactOpts) ([]TapeEntry, error) {
	// 1. Read current context (from last anchor)
	entries, err := m.Store.FetchAll(opts.Tape, &FetchOpts{LastAnchor: true})
	if err != nil {
		// No anchor, fetch all
		entries, err = m.Store.FetchAll(opts.Tape, nil)
		if err != nil {
			return nil, err
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("tape is empty, nothing to compact")
	}

	// 2. Convert to messages
	tapeCtx := NewLastAnchorContext()
	messages := tapeCtx.BuildMessages(entries)
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to compact")
	}

	// 3. Call LLM to generate summary
	summary, err := opts.Summarizer(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("summarize: %w", err)
	}

	// 4. Create anchor (pure marker)
	name := opts.AnchorName
	if name == "" {
		name = fmt.Sprintf("compact:%s", time.Now().UTC().Format("20060102-150405"))
	}
	anchor := NewAnchorEntry(name, map[string]any{
		"entries_count":  len(entries),
		"messages_count": len(messages),
	})
	if err := m.Store.Append(opts.Tape, anchor); err != nil {
		return nil, fmt.Errorf("append compact anchor: %w", err)
	}

	// 5. Create compact_summary entry (independent entry after anchor)
	summaryEntry := NewCompactSummaryEntry(summary)
	if err := m.Store.Append(opts.Tape, summaryEntry); err != nil {
		return nil, fmt.Errorf("append compact summary: %w", err)
	}

	// 6. Record compact event
	event := NewEventEntry("compact", map[string]any{
		"anchor":         name,
		"summary_length": len(summary),
	})
	if err := m.Store.Append(opts.Tape, event); err != nil {
		return nil, fmt.Errorf("append compact event: %w", err)
	}

	return []TapeEntry{anchor, summaryEntry, event}, nil
}
