package tape

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	EventName  string // optional, defaults to "compact"
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
	eventName := opts.EventName
	if eventName == "" {
		eventName = "compact"
	}
	event := NewEventEntry(eventName, map[string]any{
		"anchor":         name,
		"summary_length": len(summary),
	})
	if err := m.Store.Append(opts.Tape, event); err != nil {
		return nil, fmt.Errorf("append compact event: %w", err)
	}

	return []TapeEntry{anchor, summaryEntry, event}, nil
}

// ---------------------------------------------------------------------------
// TapeController — event-log semantics on top of TapeManager
// ---------------------------------------------------------------------------

// TapeController provides EventLog-style operations (execution tracking,
// resumption, fork) while keeping the underlying store unchanged.
type TapeController struct {
	Manager *TapeManager
}

// NewTapeController creates a TapeController backed by the given manager.
func NewTapeController(m *TapeManager) *TapeController {
	return &TapeController{Manager: m}
}

// RecordExecStart records the beginning of an execution.
func (tc *TapeController) RecordExecStart(tape, execID, agentID string, config map[string]any) error {
	return tc.Manager.AppendEntry(tape, NewExecStartEntry(execID, agentID, config))
}

// RecordExecInput records messages sent as input to an execution.
func (tc *TapeController) RecordExecInput(tape, execID string, messages []map[string]any) error {
	return tc.Manager.AppendEntry(tape, NewExecInputEntry(execID, messages))
}

// RecordExecOutput records messages produced as output from an execution.
func (tc *TapeController) RecordExecOutput(tape, execID string, messages []map[string]any) error {
	return tc.Manager.AppendEntry(tape, NewExecOutputEntry(execID, messages))
}

// RecordExecState records a state transition for an execution.
func (tc *TapeController) RecordExecState(tape, execID string, state ExecState) error {
	return tc.Manager.AppendEntry(tape, NewExecStateEntry(execID, state))
}

// ExecReplay is the result of replaying an execution's history.
type ExecReplay struct {
	ExecID   string
	AgentID  string
	Config   map[string]any
	Inputs   []map[string]any
	Outputs  []map[string]any
	State    ExecState
	Messages []map[string]any // inputs + outputs merged in order
}

// extractMessages normalizes the "messages" field from a payload into []map[string]any.
// It handles both []map[string]any (in-memory) and []any (deserialized from JSON).
func extractMessages(payload map[string]any) []map[string]any {
	var out []map[string]any

	// Try []map[string]any first (in-memory store, no JSON round-trip).
	if msgs, ok := payload["messages"].([]map[string]any); ok {
		for _, m := range msgs {
			out = append(out, m)
		}
		return out
	}

	// Fall back to []any (after JSON deserialization).
	if msgs, ok := payload["messages"].([]any); ok {
		for _, m := range msgs {
			if msgMap, ok := m.(map[string]any); ok {
				out = append(out, msgMap)
			}
		}
	}
	return out
}

// ReplayExec reconstructs an execution's history from tape entries.
// If execID is empty, it replays all exec_* entries in the tape (sub-tape mode).
func (tc *TapeController) ReplayExec(tape, execID string) (*ExecReplay, error) {
	entries, err := tc.Manager.Store.FetchAll(tape, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch entries: %w", err)
	}

	var replay ExecReplay
	replay.ExecID = execID

	for _, e := range entries {
		// When execID is set, skip entries that belong to a different execution.
		if execID != "" {
			eid, _ := e.Payload["exec_id"].(string)
			if eid != "" && eid != execID {
				continue
			}
		}

		switch e.Kind {
		case "exec_start":
			if id, ok := e.Payload["exec_id"].(string); ok && id != "" {
				replay.ExecID = id
			}
			if aid, ok := e.Payload["agent_id"].(string); ok {
				replay.AgentID = aid
			}
			if cfg, ok := e.Payload["config"].(map[string]any); ok {
				replay.Config = cfg
			}
		case "exec_input":
			for _, msgMap := range extractMessages(e.Payload) {
				replay.Inputs = append(replay.Inputs, msgMap)
				replay.Messages = append(replay.Messages, msgMap)
			}
		case "exec_output":
			for _, msgMap := range extractMessages(e.Payload) {
				replay.Outputs = append(replay.Outputs, msgMap)
				replay.Messages = append(replay.Messages, msgMap)
			}
		case "exec_state":
			if s, ok := e.Payload["state"].(string); ok {
				replay.State = ExecState(s)
			}
		}
	}

	return &replay, nil
}

// FindPendingExec scans a conversation tape and returns the exec_id of the
// most recent execution whose final state is "pending". Returns empty string
// if none found.
func (tc *TapeController) FindPendingExec(tape string) (string, error) {
	entries, err := tc.Manager.Store.FetchAll(tape, &FetchOpts{Kinds: []string{"exec_state"}})
	if err != nil {
		return "", fmt.Errorf("fetch exec_state entries: %w", err)
	}

	// Track the last known state per exec_id.
	lastState := make(map[string]ExecState)
	for _, e := range entries {
		eid, _ := e.Payload["exec_id"].(string)
		if eid == "" {
			continue
		}
		if s, ok := e.Payload["state"].(string); ok {
			lastState[eid] = ExecState(s)
		}
	}

	// Return the most recent pending exec_id (last one in tape order).
	for i := len(entries) - 1; i >= 0; i-- {
		eid, _ := entries[i].Payload["exec_id"].(string)
		if eid != "" && lastState[eid] == ExecStatePending {
			return eid, nil
		}
	}
	return "", nil
}

// ListConversations returns tape names that represent top-level conversations
// (i.e. those without an ":exec:" segment).
func (tc *TapeController) ListConversations() ([]string, error) {
	all := tc.Manager.Store.ListTapes()
	var convs []string
	for _, t := range all {
		if !strings.Contains(t, ":exec:") {
			convs = append(convs, t)
		}
	}
	return convs, nil
}

// ListExecs returns execution tape names that belong to a given conversation.
func (tc *TapeController) ListExecs(convTape string) ([]string, error) {
	prefix := convTape + ":exec:"
	all := tc.Manager.Store.ListTapes()
	var execs []string
	for _, t := range all {
		if strings.HasPrefix(t, prefix) {
			execs = append(execs, t)
		}
	}
	return execs, nil
}

// Fork copies entries with ID <= fromID from fromTape into toTape.
func (tc *TapeController) Fork(fromTape string, fromID int, toTape string) error {
	entries, err := tc.Manager.Store.FetchAll(fromTape, nil)
	if err != nil {
		return fmt.Errorf("fetch source tape: %w", err)
	}

	for _, e := range entries {
		if e.ID > fromID {
			continue
		}
		// Copy with zero ID so the store assigns a fresh one.
		copyEntry := e
		copyEntry.ID = 0
		if err := tc.Manager.Store.Append(toTape, copyEntry); err != nil {
			return fmt.Errorf("append forked entry: %w", err)
		}
	}

	// Record the fork operation itself in the destination tape.
	if err := tc.Manager.Store.Append(toTape, NewForkEntry(fromTape, fromID, toTape)); err != nil {
		return fmt.Errorf("append fork entry: %w", err)
	}
	return nil
}

// CatchUp streams entries with ID > afterID from tape to the handler.
func (tc *TapeController) CatchUp(tape string, afterID int, handler func(TapeEntry) error) error {
	entries, err := tc.Manager.Store.FetchAll(tape, nil)
	if err != nil {
		return fmt.Errorf("fetch tape entries: %w", err)
	}
	for _, e := range entries {
		if e.ID > afterID {
			if err := handler(e); err != nil {
				return fmt.Errorf("handler error at id %d: %w", e.ID, err)
			}
		}
	}
	return nil
}
