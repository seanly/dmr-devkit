package tape

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/core"
)

var (
	// entryTimezone is the timezone used for tape entry timestamps.
	// Default is local timezone. Can be set via SetTimezone().
	entryTimezone *time.Location
	entryTzMu     sync.RWMutex
)

func init() {
	entryTimezone = time.Local
}

// SetTimezone sets the timezone for tape entry timestamps.
// tzName should be an IANA timezone name (e.g., "Asia/Shanghai", "UTC").
// If tzName is empty or invalid, uses system local timezone.
func SetTimezone(tzName string) error {
	if tzName == "" {
		entryTzMu.Lock()
		entryTimezone = time.Local
		entryTzMu.Unlock()
		return nil
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return err
	}

	entryTzMu.Lock()
	entryTimezone = loc
	entryTzMu.Unlock()
	return nil
}

// GetTimezone returns the current timezone used for tape entries.
func GetTimezone() *time.Location {
	entryTzMu.RLock()
	defer entryTzMu.RUnlock()
	return entryTimezone
}

// TapeEntry is a single record in the audit trail.
type TapeEntry struct {
	ID      int            `json:"id"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
	Meta    map[string]any `json:"meta,omitempty"`
	Date    string         `json:"date"`
}

// EntryOption configures optional fields on a TapeEntry.
type EntryOption func(*TapeEntry)

// WithScope sets a scope key in Meta.
func WithScope(scope string) EntryOption {
	return func(e *TapeEntry) {
		if e.Meta == nil {
			e.Meta = map[string]any{}
		}
		e.Meta["scope"] = scope
	}
}

// WithMeta sets arbitrary Meta.
func WithMeta(meta map[string]any) EntryOption {
	return func(e *TapeEntry) {
		e.Meta = meta
	}
}

func newEntry(kind string, payload map[string]any, opts ...EntryOption) TapeEntry {
	entryTzMu.RLock()
	tz := entryTimezone
	entryTzMu.RUnlock()

	e := TapeEntry{
		Kind:    kind,
		Payload: payload,
		Date:    time.Now().In(tz).Format("2006-01-02T15:04:05.000000-07:00"),
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

func NewMessageEntry(payload map[string]any, opts ...EntryOption) TapeEntry {
	return newEntry("message", payload, opts...)
}

func NewSystemEntry(content string) TapeEntry {
	return newEntry("system", map[string]any{"content": content})
}

func NewAnchorEntry(name string, state map[string]any) TapeEntry {
	payload := map[string]any{"name": name}
	if state != nil {
		payload["state"] = state
	}
	return newEntry("anchor", payload)
}

func NewToolCallEntry(calls []map[string]any, opts ...EntryOption) TapeEntry {
	return newEntry("tool_call", map[string]any{"calls": calls}, opts...)
}

func NewToolResultEntry(results []any, opts ...EntryOption) TapeEntry {
	return newEntry("tool_result", map[string]any{"results": results}, opts...)
}

// NewContentReplacementEntry records a per-tool_call_id replacement string for audit/resume tooling.
func NewContentReplacementEntry(toolCallID, replacement string, opts ...EntryOption) TapeEntry {
	return newEntry("content_replacement", map[string]any{
		"tool_call_id": toolCallID,
		"replacement": replacement,
	}, opts...)
}

func NewErrorEntry(kind core.ErrorKind, message string) TapeEntry {
	return newEntry("error", map[string]any{"kind": string(kind), "message": message})
}

func NewEventEntry(name string, data map[string]any, opts ...EntryOption) TapeEntry {
	return newEntry("event", map[string]any{"name": name, "data": data}, opts...)
}

// NewCompactSummaryEntry creates a compact_summary entry storing an LLM-generated context summary.
func NewCompactSummaryEntry(summary string) TapeEntry {
	return newEntry("compact_summary", map[string]any{"content": summary})
}

// ToolCallData holds structured data from a tool_call entry payload.
type ToolCallData struct {
	ID        string
	Type      string
	Name      string
	Arguments string
}

// ToolResultData holds structured data from a tool_result entry payload.
type ToolResultData struct {
	Content string
}

// ExtractToolCalls extracts tool-call data from a tool_call entry payload.
// Returns false if the payload does not contain a valid "calls" field.
func ExtractToolCalls(payload map[string]any) ([]ToolCallData, bool) {
	var callsRaw []any
	switch v := payload["calls"].(type) {
	case []any:
		callsRaw = v
	case []map[string]any:
		callsRaw = make([]any, len(v))
		for i, c := range v {
			callsRaw[i] = c
		}
	default:
		return nil, false
	}
	var out []ToolCallData
	for _, c := range callsRaw {
		callMap, ok := c.(map[string]any)
		if !ok {
			continue
		}
		tc := ToolCallData{}
		if id, ok := callMap["id"].(string); ok {
			tc.ID = id
		}
		if typ, ok := callMap["type"].(string); ok {
			tc.Type = typ
		}
		if fn, ok := callMap["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				tc.Name = name
			}
			if args, ok := fn["arguments"].(string); ok {
				tc.Arguments = args
			}
		}
		out = append(out, tc)
	}
	return out, true
}

// FormatToolResultItem turns one executed tool output value into text for LLM transport.
// It mirrors the normalization used when recording tape tool_result payloads.
func FormatToolResultItem(r any) string {
	return toolResultItemToString(r)
}

// toolResultItemToString turns one element of a tool_result "results" array into display text.
// Tape may store OpenAI-style {"content": "..."}, plain strings from handlers, error maps
// {"kind","message"}, numbers, or other JSON-serializable values.
func toolResultItemToString(r any) string {
	if r == nil {
		return ""
	}
	switch v := r.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprint(v)
	case int:
		return fmt.Sprint(v)
	case int64:
		return fmt.Sprint(v)
	case json.Number:
		return v.String()
	case map[string]any:
		if c, ok := v["content"].(string); ok {
			return c
		}
		if msg, ok := v["message"].(string); ok {
			if kind, _ := v["kind"].(string); kind != "" {
				return kind + ": " + msg
			}
			return msg
		}
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	default:
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	}
}

// ExtractToolResults extracts tool-result data from a tool_result entry payload.
// Returns false if the payload does not contain a valid "results" field.
func ExtractToolResults(payload map[string]any) ([]ToolResultData, bool) {
	resultsRaw, ok := payload["results"].([]any)
	if !ok {
		return nil, false
	}
	out := make([]ToolResultData, 0, len(resultsRaw))
	for _, r := range resultsRaw {
		out = append(out, ToolResultData{Content: toolResultItemToString(r)})
	}
	return out, true
}

// ---------------------------------------------------------------------------
// Execution lifecycle entries (event-log semantics)
// ---------------------------------------------------------------------------

// ExecState represents the execution state of an agent run.
type ExecState string

const (
	ExecStatePending   ExecState = "pending"
	ExecStateCompleted ExecState = "completed"
	ExecStateFailed    ExecState = "failed"
)

// NewExecStartEntry records the start of an execution.
func NewExecStartEntry(execID, agentID string, config map[string]any, opts ...EntryOption) TapeEntry {
	payload := map[string]any{
		"exec_id":  execID,
		"agent_id": agentID,
	}
	if config != nil {
		payload["config"] = config
	}
	return newEntry("exec_start", payload, opts...)
}

// NewExecInputEntry records inputs (messages) sent to an agent during execution.
func NewExecInputEntry(execID string, messages []map[string]any, opts ...EntryOption) TapeEntry {
	return newEntry("exec_input", map[string]any{
		"exec_id":  execID,
		"messages": messages,
	}, opts...)
}

// NewExecOutputEntry records outputs (messages) produced by an agent during execution.
func NewExecOutputEntry(execID string, messages []map[string]any, opts ...EntryOption) TapeEntry {
	return newEntry("exec_output", map[string]any{
		"exec_id":  execID,
		"messages": messages,
	}, opts...)
}

// NewExecStateEntry records a state transition for an execution.
func NewExecStateEntry(execID string, state ExecState, opts ...EntryOption) TapeEntry {
	return newEntry("exec_state", map[string]any{
		"exec_id": execID,
		"state":   string(state),
	}, opts...)
}

// NewForkEntry records a fork operation from one tape to another.
func NewForkEntry(fromTape string, fromID int, toTape string, opts ...EntryOption) TapeEntry {
	return newEntry("fork", map[string]any{
		"from_tape": fromTape,
		"from_id":   fromID,
		"to_tape":   toTape,
	}, opts...)
}
