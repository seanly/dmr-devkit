package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tool"
)

// TextClient provides structured text operations (If, Classify).
type TextClient struct {
	chat                  *ChatClient
	unsupportedToolChoice map[string]bool // Cache of models that don't support forced tool_choice
	mu                    sync.RWMutex
}

// NewTextClient creates a new TextClient.
func NewTextClient(chat *ChatClient) *TextClient {
	return &TextClient{chat: chat}
}

// TextOpts configures text operations.
type TextOpts struct {
	SystemPrompt string
	MaxTokens    int
	Temperature  *float32
	Tape         string
}

// If asks the LLM a yes/no question about the input.
func (t *TextClient) If(ctx context.Context, input, question string, opts ...TextOpts) (bool, error) {
	var o TextOpts
	if len(opts) > 0 {
		o = opts[0]
	}

	ifTool := &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "if_decision",
			Description: "Return your yes/no decision",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{
						"type":        "boolean",
						"description": "true for yes, false for no",
					},
				},
				"required": []string{"value"},
			},
		},
	}

	prompt := fmt.Sprintf("Input: %s\n\nQuestion: %s\n\nYou MUST respond using the if_decision tool with a boolean value. Do not provide a text response.", input, question)

	// Get model name for cache lookup
	model := t.chat.Core.Model()

	// Check cache: skip forced tool_choice if this model doesn't support it
	var calls []core.ToolCallData
	var err error
	if t.shouldSkipToolChoice(model) {
		// Use auto mode directly
		calls, err = t.chat.ToolCalls(ctx, ChatOpts{
			Prompt:       prompt,
			SystemPrompt: o.SystemPrompt,
			Tools:        []*tool.Tool{ifTool},
			ToolChoice:   nil,
			MaxTokens:    o.MaxTokens,
			Tape:         o.Tape,
		})
	} else {
		// Try with forced tool choice first
		calls, err = t.chat.ToolCalls(ctx, ChatOpts{
			Prompt:       prompt,
			SystemPrompt: o.SystemPrompt,
			Tools:        []*tool.Tool{ifTool},
			ToolChoice:   map[string]any{"type": "function", "function": map[string]any{"name": "if_decision"}},
			MaxTokens:    o.MaxTokens,
			Tape:         o.Tape,
		})

		// If error mentions tool_choice not supported, cache and retry
		if err != nil && t.isToolChoiceError(err) {
			t.markUnsupportedToolChoice(model)
			calls, err = t.chat.ToolCalls(ctx, ChatOpts{
				Prompt:       prompt,
				SystemPrompt: o.SystemPrompt,
				Tools:        []*tool.Tool{ifTool},
				ToolChoice:   nil,
				MaxTokens:    o.MaxTokens,
				Tape:         o.Tape,
			})
		}
	}

	if err != nil {
		return false, err
	}

	if len(calls) == 0 {
		return false, core.NewError(core.ErrInvalidInput, "no tool call returned for If decision", nil)
	}

	var result struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &result); err != nil {
		return false, core.NewError(core.ErrInvalidInput, "failed to parse If decision: "+err.Error(), err)
	}

	return result.Value, nil
}

// Classify asks the LLM to classify the input into one of the given choices.
func (t *TextClient) Classify(ctx context.Context, input string, choices []string, opts ...TextOpts) (string, error) {
	var o TextOpts
	if len(opts) > 0 {
		o = opts[0]
	}

	enumChoices := make([]any, len(choices))
	for i, c := range choices {
		enumChoices[i] = c
	}

	classifyTool := &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "classify_decision",
			Description: "Return your classification decision",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label": map[string]any{
						"type":        "string",
						"enum":        enumChoices,
						"description": "The classification label",
					},
				},
				"required": []string{"label"},
			},
		},
	}

	choiceList := ""
	for i, c := range choices {
		if i > 0 {
			choiceList += ", "
		}
		choiceList += c
	}
	prompt := fmt.Sprintf("Input: %s\n\nClassify into one of: [%s]\n\nYou MUST respond using the classify_decision tool with one of the provided labels. Do not provide a text response.", input, choiceList)

	// Get model name for cache lookup
	model := t.chat.Core.Model()

	// Check cache: skip forced tool_choice if this model doesn't support it
	var calls []core.ToolCallData
	var err error
	if t.shouldSkipToolChoice(model) {
		// Use auto mode directly
		calls, err = t.chat.ToolCalls(ctx, ChatOpts{
			Prompt:       prompt,
			SystemPrompt: o.SystemPrompt,
			Tools:        []*tool.Tool{classifyTool},
			ToolChoice:   nil,
			MaxTokens:    o.MaxTokens,
			Tape:         o.Tape,
		})
	} else {
		// Try with forced tool choice first
		calls, err = t.chat.ToolCalls(ctx, ChatOpts{
			Prompt:       prompt,
			SystemPrompt: o.SystemPrompt,
			Tools:        []*tool.Tool{classifyTool},
			ToolChoice:   map[string]any{"type": "function", "function": map[string]any{"name": "classify_decision"}},
			MaxTokens:    o.MaxTokens,
			Tape:         o.Tape,
		})

		// If error mentions tool_choice not supported, cache and retry
		if err != nil && t.isToolChoiceError(err) {
			t.markUnsupportedToolChoice(model)
			calls, err = t.chat.ToolCalls(ctx, ChatOpts{
				Prompt:       prompt,
				SystemPrompt: o.SystemPrompt,
				Tools:        []*tool.Tool{classifyTool},
				ToolChoice:   nil,
				MaxTokens:    o.MaxTokens,
				Tape:         o.Tape,
			})
		}
	}

	if err != nil {
		return "", err
	}

	if len(calls) == 0 {
		return "", core.NewError(core.ErrInvalidInput, "no tool call returned for Classify", nil)
	}

	var result struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &result); err != nil {
		return "", core.NewError(core.ErrInvalidInput, "failed to parse Classify decision: "+err.Error(), err)
	}

	// Validate label is in choices
	valid := false
	for _, c := range choices {
		if c == result.Label {
			valid = true
			break
		}
	}
	if !valid {
		return "", core.NewError(core.ErrInvalidInput,
			fmt.Sprintf("label %q not in choices %v", result.Label, choices), nil)
	}

	return result.Label, nil
}

// shouldSkipToolChoice checks if the model is known to not support forced tool_choice.
func (t *TextClient) shouldSkipToolChoice(model string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.unsupportedToolChoice[model]
}

// markUnsupportedToolChoice marks a model as not supporting forced tool_choice.
func (t *TextClient) markUnsupportedToolChoice(model string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.unsupportedToolChoice == nil {
		t.unsupportedToolChoice = make(map[string]bool)
	}
	t.unsupportedToolChoice[model] = true
}

// isToolChoiceError checks if the error is related to tool_choice not being supported.
func (t *TextClient) isToolChoiceError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "tool_choice") ||
		(strings.Contains(errMsg, "thinking") && strings.Contains(errMsg, "mode"))
}
