package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/agent/toolresult"
	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/provider"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// ChatClient is the core conversation engine.
type ChatClient struct {
	Core     *core.LLMCore
	Executor *tool.ToolExecutor
	Tape     *tape.TapeManager
}

// ChatOpts holds the options for a chat request.
type ChatOpts struct {
	Prompt       string
	Messages     []map[string]any
	SystemPrompt string
	Tools        []*tool.Tool
	ToolChoice   any
	ToolContext  *tool.ToolContext
	MaxTokens    int
	Temperature  *float32
	TopP         *float32
	Tape         string
	Context      *tape.TapeContext
	// HistoryAfterEntryID, if > 0, loads tape messages from rows with id > this (matches web "From" anchor).
	// When set, Context is ignored for tape reads.
	HistoryAfterEntryID int
	MaxToolRounds       int
	ExtraHeaders        map[string]string
	// ToolResultManager, when set with Tape, runs merge + microcompact on tape-loaded history before each request.
	ToolResultManager *toolresult.Manager
}

// StreamState holds the accumulated state from a streaming response.
type StreamState struct {
	Usage map[string]any
	Error *core.ErrorPayload
}

// NewChatClient creates a new ChatClient.
func NewChatClient(c *core.LLMCore, executor *tool.ToolExecutor, tm *tape.TapeManager) *ChatClient {
	return &ChatClient{Core: c, Executor: executor, Tape: tm}
}

// Chat performs a non-streaming chat and returns the text response.
func (c *ChatClient) Chat(ctx context.Context, opts ChatOpts) (string, error) {
	prepared, err := c.prepare(opts)
	if err != nil {
		return "", err
	}

	resp, err := c.Core.RunChat(ctx, core.RunChatOpts{
		Messages:     prepared.messages,
		Tools:        prepared.schemas,
		ToolChoice:   opts.ToolChoice,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		TopP:         opts.TopP,
		ExtraHeaders: opts.ExtraHeaders,
	})
	if err != nil {
		c.recordError(opts, err)
		return "", err
	}

	c.recordSuccess(opts, resp, nil)
	return resp.Text, nil
}

// ToolCalls performs a chat and returns the tool calls.
func (c *ChatClient) ToolCalls(ctx context.Context, opts ChatOpts) ([]core.ToolCallData, error) {
	prepared, err := c.prepare(opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.Core.RunChat(ctx, core.RunChatOpts{
		Messages:     prepared.messages,
		Tools:        prepared.schemas,
		ToolChoice:   opts.ToolChoice,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		TopP:         opts.TopP,
		ExtraHeaders: opts.ExtraHeaders,
	})
	if err != nil {
		return nil, err
	}

	calls := convertToolCalls(resp.ToolCalls)
	calls = expandConcatenatedToolCalls(calls)
	return calls, nil
}

// RunTools performs a chat, executes tool calls automatically, and returns the result.
func (c *ChatClient) RunTools(ctx context.Context, opts ChatOpts) (*core.ToolAutoResult, error) {
	prepared, err := c.prepare(opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.Core.RunChat(ctx, core.RunChatOpts{
		Messages:     prepared.messages,
		Tools:        prepared.schemas,
		ToolChoice:   opts.ToolChoice,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		TopP:         opts.TopP,
		ExtraHeaders: opts.ExtraHeaders,
	})
	if err != nil {
		return &core.ToolAutoResult{Kind: "error", Error: &core.ErrorPayload{
			Kind: core.ErrProvider, Message: err.Error(),
		}}, err
	}

	usage := usageMap(resp.Usage)
	calls := convertToolCalls(resp.ToolCalls)
	calls = expandConcatenatedToolCalls(calls)

	if len(calls) == 0 {
		return &core.ToolAutoResult{Kind: "text", Text: resp.Text, Reasoning: resp.Reasoning, Usage: usage}, nil
	}

	if prepared.toolSet == nil {
		return &core.ToolAutoResult{Kind: "tools", Text: resp.Text, Reasoning: resp.Reasoning, ToolCalls: calls, Usage: usage}, nil
	}

	execution := c.Executor.Execute(calls, prepared.toolSet, opts.ToolContext)
	return &core.ToolAutoResult{
		Kind:        "tools",
		Text:        resp.Text,
		Reasoning:   resp.Reasoning,
		ToolCalls:   execution.ToolCalls,
		ToolResults: execution.ToolResults,
		Error:       execution.Error,
		Usage:       usage,
	}, nil
}

// Stream returns a channel of text chunks.
func (c *ChatClient) Stream(ctx context.Context, opts ChatOpts) (<-chan string, *StreamState, error) {
	prepared, err := c.prepare(opts)
	if err != nil {
		return nil, nil, err
	}

	chunkCh, err := c.Core.RunChatStream(ctx, core.RunChatOpts{
		Messages:     prepared.messages,
		Tools:        prepared.schemas,
		ToolChoice:   opts.ToolChoice,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		TopP:         opts.TopP,
		ExtraHeaders: opts.ExtraHeaders,
	})
	if err != nil {
		return nil, nil, err
	}

	state := &StreamState{}
	textCh := make(chan string, 32)

	go func() {
		defer close(textCh)
		tf := &thinkFilter{}
		for chunk := range chunkCh {
			if chunk.Err != nil {
				state.Error = &core.ErrorPayload{
					Kind: core.ErrProvider, Message: chunk.Err.Error(),
				}
				return
			}
			if chunk.Text != "" {
				if cleaned := tf.Feed(chunk.Text); cleaned != "" {
					select {
					case textCh <- cleaned:
					case <-ctx.Done():
						return
					}
				}
			}
			if chunk.Usage != nil {
				state.Usage = map[string]any{
					"prompt_tokens":     chunk.Usage.PromptTokens,
					"completion_tokens": chunk.Usage.CompletionTokens,
					"total_tokens":      chunk.Usage.TotalTokens,
				}
			}
		}
		if rest := tf.Flush(); rest != "" {
			select {
			case textCh <- rest:
			case <-ctx.Done():
			}
		}
	}()

	return textCh, state, nil
}

// StreamEvents returns a channel of structured events.
func (c *ChatClient) StreamEvents(ctx context.Context, opts ChatOpts) (<-chan core.StreamEvent, *StreamState, error) {
	prepared, err := c.prepare(opts)
	if err != nil {
		return nil, nil, err
	}

	chunkCh, err := c.Core.RunChatStream(ctx, core.RunChatOpts{
		Messages:     prepared.messages,
		Tools:        prepared.schemas,
		ToolChoice:   opts.ToolChoice,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		TopP:         opts.TopP,
		ExtraHeaders: opts.ExtraHeaders,
	})
	if err != nil {
		return nil, nil, err
	}

	state := &StreamState{}
	eventCh := make(chan core.StreamEvent, 32)

	go func() {
		defer close(eventCh)
		assembler := newToolCallAssembler()
		tf := &thinkFilter{}
		var allText string
		var lastUsage map[string]any

		for chunk := range chunkCh {
			if chunk.Err != nil {
				state.Error = &core.ErrorPayload{
					Kind: core.ErrProvider, Message: chunk.Err.Error(),
				}
				select {
				case eventCh <- core.StreamEvent{
					Kind: core.StreamError,
					Data: map[string]any{"message": chunk.Err.Error()},
				}:
				case <-ctx.Done():
				}
				return
			}

			if chunk.Text != "" {
				cleaned := tf.Feed(chunk.Text)
				if cleaned != "" {
					allText += cleaned
					select {
					case eventCh <- core.StreamEvent{
						Kind: core.StreamText,
						Data: map[string]any{"delta": cleaned},
					}:
					case <-ctx.Done():
						return
					}
				}
			}

			for _, tc := range chunk.ToolCalls {
				assembler.addDelta(tc)
			}

			if chunk.Usage != nil {
				lastUsage = map[string]any{
					"prompt_tokens":     chunk.Usage.PromptTokens,
					"completion_tokens": chunk.Usage.CompletionTokens,
					"total_tokens":      chunk.Usage.TotalTokens,
				}
				state.Usage = lastUsage
				select {
				case eventCh <- core.StreamEvent{
					Kind: core.StreamUsage,
					Data: lastUsage,
				}:
				case <-ctx.Done():
					return
				}
			}
		}

		// Flush any buffered partial text from the think filter.
		if rest := tf.Flush(); rest != "" {
			allText += rest
			select {
			case eventCh <- core.StreamEvent{
				Kind: core.StreamText,
				Data: map[string]any{"delta": rest},
			}:
			case <-ctx.Done():
				return
			}
		}

		// Emit completed tool calls
		completedCalls := assembler.complete()
		completedCalls = expandConcatenatedToolCalls(completedCalls)

		var toolResults []any
		for _, call := range completedCalls {
			callMap := map[string]any{
				"id":       call.ID,
				"function": map[string]any{"name": call.Function.Name, "arguments": call.Function.Arguments},
			}
			select {
			case eventCh <- core.StreamEvent{
				Kind: core.StreamToolCall,
				Data: map[string]any{"call": callMap},
			}:
			case <-ctx.Done():
				return
			}

			// Execute tool if available
			if prepared.toolSet != nil {
				if t, ok := prepared.toolSet.Runnable[call.Function.Name]; ok && t.Handler != nil {
					execution := c.Executor.Execute(
						[]core.ToolCallData{call},
						prepared.toolSet,
						opts.ToolContext,
					)
					if len(execution.ToolResults) > 0 {
						result := execution.ToolResults[0]
						toolResults = append(toolResults, result)
						select {
						case eventCh <- core.StreamEvent{
							Kind: core.StreamToolResult,
							Data: map[string]any{"result": result},
						}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}

		// Final event
		finalToolCalls := make([]any, 0, len(completedCalls))
		for _, call := range completedCalls {
			finalToolCalls = append(finalToolCalls, []any{call.ID, call.Function.Name, call.Function.Arguments})
		}

		finalData := map[string]any{
			"text":         nilIfEmpty(allText),
			"tool_calls":   completedCalls,
			"tool_results": toolResults,
			"usage":        lastUsage,
			"error":        nil,
		}
		if state.Error != nil {
			finalData["error"] = state.Error
		}
		select {
		case eventCh <- core.StreamEvent{
			Kind: core.StreamFinal,
			Data: finalData,
		}:
		case <-ctx.Done():
		}
	}()

	return eventCh, state, nil
}

// collapseSystemMessages merges every role=system message into a single message at
// the front (content joined with "\n\n"), skipping empty bodies and omitting a
// segment when it equals the previous appended segment after TrimSpace (so tape
// + prepare duplicates collapse to one block).
func collapseSystemMessages(msgs []map[string]any) []map[string]any {
	if len(msgs) == 0 {
		return msgs
	}
	var parts []string
	nonSystem := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		role, _ := m["role"].(string)
		if role != "system" {
			nonSystem = append(nonSystem, m)
			continue
		}
		content := strings.TrimSpace(systemMessageContent(m))
		if content == "" {
			continue
		}
		if len(parts) > 0 && parts[len(parts)-1] == content {
			continue
		}
		parts = append(parts, content)
	}
	if len(parts) == 0 {
		return nonSystem
	}
	merged := strings.Join(parts, "\n\n")
	out := make([]map[string]any, 0, 1+len(nonSystem))
	out = append(out, map[string]any{"role": "system", "content": merged})
	out = append(out, nonSystem...)
	return out
}

func systemMessageContent(m map[string]any) string {
	if m == nil {
		return ""
	}
	switch c := m["content"].(type) {
	case string:
		return c
	default:
		if c == nil {
			return ""
		}
		return fmt.Sprint(c)
	}
}

// prepare builds messages from prompt/tape/context.
type preparedChat struct {
	messages []map[string]any
	schemas  []map[string]any
	toolSet  *tool.ToolSet
}

func (c *ChatClient) prepare(opts ChatOpts) (*preparedChat, error) {
	p := &preparedChat{}

	var msgs []map[string]any
	if opts.Messages != nil {
		msgs = opts.Messages
	} else {
		if opts.Tape != "" && c.Tape != nil {
			var tapeMsgs []map[string]any
			var err error
			if opts.HistoryAfterEntryID > 0 {
				entries, err := c.Tape.Store.FetchAll(opts.Tape, &tape.FetchOpts{AfterID: opts.HistoryAfterEntryID})
				if err != nil {
					return nil, core.NewError(core.ErrUnknown, "failed to read tape entries", err)
				}
				tapeMsgs = tape.NewNoAnchorContext().BuildMessages(entries)
			} else {
				ctx := opts.Context
				if ctx == nil {
					ctx = tape.NewLastAnchorContext()
				}
				tapeMsgs, err = c.Tape.ReadMessages(opts.Tape, ctx)
				if err != nil {
					return nil, core.NewError(core.ErrUnknown, "failed to read tape messages", err)
				}
			}
			if opts.ToolResultManager != nil {
				opts.ToolResultManager.PrepareWireMessages(opts.Tape, tapeMsgs, time.Now())
			}
			msgs = append(msgs, tapeMsgs...)
		}

		// Add system prompt
		if opts.SystemPrompt != "" {
			msgs = append([]map[string]any{{"role": "system", "content": opts.SystemPrompt}}, msgs...)
		}

		// Add current prompt
		if opts.Prompt != "" {
			msgs = append(msgs, map[string]any{"role": "user", "content": opts.Prompt})
		}
	}
	p.messages = collapseSystemMessages(msgs)

	// Normalize tools
	if len(opts.Tools) > 0 {
		ts, err := tool.NormalizeTools(opts.Tools)
		if err != nil {
			return nil, core.NewError(core.ErrInvalidInput, "failed to normalize tools", err)
		}
		p.toolSet = ts
		p.schemas = ts.Schemas
	}

	return p, nil
}

func (c *ChatClient) recordError(opts ChatOpts, err error) {
	if opts.Tape == "" || c.Tape == nil {
		return
	}
	var kind core.ErrorKind
	if re, ok := err.(*core.RepublicError); ok {
		kind = re.Kind
	} else {
		kind = core.ErrUnknown
	}
	c.Tape.RecordChat(tape.RecordChatOpts{
		Tape:     opts.Tape,
		Messages: opts.Messages,
		Error:    &core.ErrorPayload{Kind: kind, Message: err.Error()},
	})
}

func (c *ChatClient) recordSuccess(opts ChatOpts, resp *provider.ChatResponse, toolResults []any) {
	if opts.Tape == "" || c.Tape == nil {
		return
	}
	var usage map[string]any
	if resp.Usage != nil {
		usage = map[string]any{
			"total_tokens": resp.Usage.TotalTokens,
		}
	}
	userMsgs := []map[string]any{{"role": "user", "content": opts.Prompt}}
	assistantEntry := map[string]any{"role": "assistant", "content": resp.Text}
	if resp.Reasoning != "" {
		assistantEntry["reasoning"] = resp.Reasoning
	}
	assistantMsgs := []map[string]any{assistantEntry}
	msgs := append(userMsgs, assistantMsgs...)
	c.Tape.RecordChat(tape.RecordChatOpts{
		Tape:         opts.Tape,
		SystemPrompt: opts.SystemPrompt,
		Messages:     msgs,
		Usage:        usage,
	})
}

// usageMap converts openai Usage to a map for ToolAutoResult.
func usageMap(u *provider.Usage) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"prompt_tokens":     u.PromptTokens,
		"completion_tokens": u.CompletionTokens,
		"total_tokens":      u.TotalTokens,
	}
}

// convertToolCalls converts from openai ToolCall to core ToolCallData.
func convertToolCalls(calls []provider.ToolCall) []core.ToolCallData {
	result := make([]core.ToolCallData, 0, len(calls))
	for _, tc := range calls {
		result = append(result, core.ToolCallData{
			ID:       tc.ID,
			Function: core.ToolCallFunction{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
		})
	}
	return result
}

// expandConcatenatedToolCalls splits concatenated JSON objects in arguments.
func expandConcatenatedToolCalls(calls []core.ToolCallData) []core.ToolCallData {
	var expanded []core.ToolCallData
	for _, call := range calls {
		parts := splitConcatenatedJSON(call.Function.Arguments)
		if len(parts) <= 1 {
			expanded = append(expanded, call)
			continue
		}
		for i, part := range parts {
			newCall := core.ToolCallData{
				ID:       call.ID,
				Function: core.ToolCallFunction{Name: call.Function.Name, Arguments: part},
			}
			if i > 0 {
				newCall.ID = call.ID + "__" + itoa(i+1)
			}
			expanded = append(expanded, newCall)
		}
	}
	return expanded
}

// splitConcatenatedJSON splits strings like '{"a":1}{"b":2}' into individual JSON objects.
func splitConcatenatedJSON(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '{' {
		return []string{s}
	}

	var parts []string
	decoder := json.NewDecoder(strings.NewReader(s))
	for decoder.More() {
		var v json.RawMessage
		if err := decoder.Decode(&v); err != nil {
			return []string{s}
		}
		parts = append(parts, string(v))
	}

	if len(parts) == 0 {
		return []string{s}
	}
	return parts
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// toolCallAssembler merges streaming tool call deltas.
type toolCallAssembler struct {
	calls []assemblerCall
}

type assemblerCall struct {
	id   string
	name string
	args strings.Builder
}

func newToolCallAssembler() *toolCallAssembler {
	return &toolCallAssembler{}
}

func (a *toolCallAssembler) addDelta(tc provider.ToolCall) {
	// Find existing call by ID or index
	idx := -1
	if tc.ID != "" {
		for i, c := range a.calls {
			if c.id == tc.ID {
				idx = i
				break
			}
		}
	}

	if idx == -1 {
		// New call or continuation without ID
		if tc.ID != "" {
			a.calls = append(a.calls, assemblerCall{id: tc.ID, name: tc.Function.Name})
			idx = len(a.calls) - 1
		} else if len(a.calls) > 0 {
			// Continuation of last call
			idx = len(a.calls) - 1
		} else {
			return
		}
	}

	if tc.Function.Name != "" && a.calls[idx].name == "" {
		a.calls[idx].name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		a.calls[idx].args.WriteString(tc.Function.Arguments)
	}
}

func (a *toolCallAssembler) complete() []core.ToolCallData {
	result := make([]core.ToolCallData, 0, len(a.calls))
	for _, c := range a.calls {
		result = append(result, core.ToolCallData{
			ID: c.id,
			Function: core.ToolCallFunction{
				Name:      c.name,
				Arguments: c.args.String(),
			},
		})
	}
	return result
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// thinkFilter is a streaming state machine that buffers and strips <think>...</think>
// blocks from text deltas. Content inside think tags is silently dropped so the
// user never sees model reasoning that was inlined in the content field.
type thinkFilter struct {
	inside bool            // currently inside a <think> block
	buf    strings.Builder // partial tag buffer
}

// Feed processes a text delta and returns the portion that should be forwarded
// to the user. Content inside <think>...</think> is consumed and discarded.
func (f *thinkFilter) Feed(delta string) string {
	var out strings.Builder
	for i := 0; i < len(delta); i++ {
		ch := delta[i]
		if f.inside {
			f.buf.WriteByte(ch)
			if strings.HasSuffix(f.buf.String(), "</think>") {
				f.inside = false
				f.buf.Reset()
			}
			continue
		}
		// Not inside a think block — look for opening tag.
		if ch == '<' {
			f.buf.WriteByte(ch)
			continue
		}
		if f.buf.Len() > 0 {
			f.buf.WriteByte(ch)
			candidate := f.buf.String()
			if candidate == "<think>" {
				f.inside = true
				f.buf.Reset()
				continue
			}
			// Still a valid prefix of "<think>"?
			const tag = "<think>"
			if len(candidate) <= len(tag) && tag[:len(candidate)] == candidate {
				continue
			}
			// Not a match — flush buffered bytes as normal text.
			out.WriteString(candidate)
			f.buf.Reset()
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

// Flush returns any buffered bytes that were not part of a complete tag.
// Call this after the stream ends.
func (f *thinkFilter) Flush() string {
	s := f.buf.String()
	f.buf.Reset()
	return s
}
