package devkit

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/workflow"
)

const toolTraceMaxRunes = 12000

func truncateDisplayRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "\n… [truncated]"
}

// AgentNode wraps a devkit [*Kit].Agent into a [workflow.Node] so it can be
// orchestrated by Sequential, Parallel, Graph, or custom workflows.
type AgentNode struct {
	Kit          *Kit
	AgentName    string
	TapeName     string
	SystemPrompt string
	// AllowedTools restricts tool visibility when non-nil: *slice may be empty to expose no tools.
	// When nil (YAML omit), eligible tools remain unrestricted after discovery.
	AllowedTools *[]string
	// Model, when non-empty, selects the ChatClient for TapeName via [agent.Agent.SwitchModel].
	Model string
}

// Name returns the node name; satisfies [workflow.Node].
func (a *AgentNode) Name() string {
	if a.AgentName != "" {
		return a.AgentName
	}
	return a.TapeName
}

// RunEvents executes the agent and emits workflow events including UI widget events.
// It satisfies [workflow.EventStream] so the node can be used with streaming runners.
func (a *AgentNode) RunEvents(ctx context.Context, wctx *workflow.Context, input any) iter.Seq2[*workflow.Event, error] {
	return func(yield func(*workflow.Event, error) bool) {
		name := a.Name()

		if !yield(&workflow.Event{Type: workflow.EventTypeWorkflowStart, Workflow: name, Step: wctx.Step, Timestamp: time.Now()}, nil) {
			return
		}
		if !yield(&workflow.Event{Type: workflow.EventTypeNodeStart, Workflow: name, Node: name, Step: wctx.Step, Timestamp: time.Now()}, nil) {
			return
		}

		prompt := ""
		switch v := input.(type) {
		case string:
			prompt = v
		case map[string]any:
			if p, ok := v["prompt"].(string); ok {
				prompt = p
			} else {
				prompt = fmt.Sprintf("%v", v)
			}
		default:
			prompt = fmt.Sprintf("%v", input)
		}

		if a.SystemPrompt != "" && wctx != nil {
			wctx.SetState(a.Name()+".system_prompt", a.SystemPrompt)
		}

		a.Kit.Agent.SetOnToolCall(func(ev agent.ToolCallEvent) {
			payload := &workflow.ToolCallPayload{
				Name:      ev.Name,
				Arguments: truncateDisplayRunes(ev.Arguments, toolTraceMaxRunes),
				Result:    truncateDisplayRunes(ev.Result, toolTraceMaxRunes),
			}
			_ = yield(&workflow.Event{
				Type:      workflow.EventTypeToolCall,
				Workflow:  name,
				Node:      name,
				Step:      wctx.Step,
				ToolCall:  payload,
				Timestamp: time.Now(),
			}, nil)
		})

		// Capture UI widgets emitted during agent run.
		a.Kit.Agent.SetOnUIWidget(func(widget any) {
			_ = yield(&workflow.Event{
				Type:      workflow.EventTypeUIWidget,
				Workflow:  name,
				Node:      name,
				Step:      wctx.Step,
				UIWidget:  widget,
				Timestamp: time.Now(),
			}, nil)
		})

		res, err := a.runInvocation(ctx, wctx, prompt)

		// Restore previous callbacks.
		a.Kit.Agent.SetOnToolCall(nil)
		a.Kit.Agent.SetOnUIWidget(nil)

		endEv := &workflow.Event{
			Type:      workflow.EventTypeNodeEnd,
			Workflow:  name,
			Node:      name,
			Step:      wctx.Step,
			Timestamp: time.Now(),
		}
		var output any
		if res != nil {
			output = res.Output
			endEv.Output = res.Output
		}
		if err != nil {
			endEv.Error = err.Error()
		}
		if !yield(endEv, nil) {
			return
		}

		final := &workflow.Result{Output: output, Error: err, Steps: wctx.Step}
		_ = yield(&workflow.Event{
			Type:      workflow.EventTypeWorkflowEnd,
			Workflow:  name,
			Step:      wctx.Step,
			Result:    final,
			Timestamp: time.Now(),
		}, nil)
	}
}

// Run executes the agent with the given input (expected string prompt).
func (a *AgentNode) Run(ctx context.Context, wctx *workflow.Context, input any) (any, error) {
	prompt := ""
	switch v := input.(type) {
	case string:
		prompt = v
	case map[string]any:
		// If input is a map, try to find a "prompt" key, otherwise serialize.
		if p, ok := v["prompt"].(string); ok {
			prompt = p
		} else {
			prompt = fmt.Sprintf("%v", v)
		}
	default:
		prompt = fmt.Sprintf("%v", input)
	}

	res, err := a.runInvocation(ctx, wctx, prompt)
	if err != nil {
		return nil, err
	}
	return res.Output, nil
}

// runInvocation executes one agent turn with optional tool restriction, optional
// per-step system prompt (via RunWithOpts/contextJSON), and optional model routing.
func (a *AgentNode) runInvocation(ctx context.Context, wctx *workflow.Context, prompt string) (*agent.Result, error) {
	tapeName := resolveWorkflowTape(a.TapeName, wctx)
	var ctxJSON string
	if s := strings.TrimSpace(a.SystemPrompt); s != "" {
		payload := map[string]any{agent.ContextKeySystemPromptOverride: s}
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("devkit workflow node: marshal context: %w", err)
		}
		ctxJSON = string(b)
	}
	if m := strings.TrimSpace(a.Model); m != "" {
		if err := a.Kit.Agent.SwitchModel(tapeName, m); err != nil {
			return nil, fmt.Errorf("devkit workflow node: SwitchModel(%q): %w", m, err)
		}
	}
	return a.Kit.Agent.RunWithOptsAndTools(ctx, tapeName, prompt, 0, 0, a.AllowedTools, ctxJSON)
}

// RunWorkflow executes a [workflow.Runner] (Sequential, Parallel, Graph, etc.)
// using this Kit's agent and tape infrastructure.
//
// It creates a workflow.Context with metadata pointing back to the Kit so that
// custom nodes can access tape, agent, and hooks via the Context.State/Metadata.
func (k *Kit) RunWorkflow(ctx context.Context, runner workflow.Runner, input any) (*workflow.Result, error) {
	wctx := workflow.NewContext()
	wctx.Metadata["run_id"] = newWorkflowRunID()
	wctx.Metadata["kit"] = k
	wctx.Metadata["tape_manager"] = k.TapeManager
	wctx.Metadata["agent"] = k.Agent
	wctx.Metadata["hooks"] = k.Hooks
	out, err := runner.Run(ctx, wctx, input)
	res, ok := out.(*workflow.Result)
	if !ok {
		return nil, fmt.Errorf("devkit: workflow runner returned %T, expected *workflow.Result", out)
	}
	return res, err
}

// RunWorkflowStream executes an [workflow.EventStream] and yields execution
// events for real-time observation.
func (k *Kit) RunWorkflowStream(ctx context.Context, runner workflow.EventStream, input any) iter.Seq2[*workflow.Event, error] {
	return func(yield func(*workflow.Event, error) bool) {
		wctx := workflow.NewContext()
		wctx.Metadata["run_id"] = newWorkflowRunID()
		wctx.Metadata["kit"] = k
		wctx.Metadata["tape_manager"] = k.TapeManager
		wctx.Metadata["agent"] = k.Agent
		wctx.Metadata["hooks"] = k.Hooks
		for ev, err := range runner.RunEvents(ctx, wctx, input) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

// AgentNodeResult wraps an [agent.Result] so it satisfies [workflow.Node].
// It is useful when you want the full Result (including Steps, Token counts)
// rather than just the string Output.
type AgentNodeResult struct {
	Res *agent.Result
}

// AsAgentNode converts this Kit into a [workflow.Node] using the default tape.
func (k *Kit) AsAgentNode(name string) *AgentNode {
	return &AgentNode{
		Kit:       k,
		AgentName: name,
		TapeName:  DefaultTapeName,
	}
}

// AsAgentNodeWithTape converts this Kit into a [workflow.Node] with a specific tape.
func (k *Kit) AsAgentNodeWithTape(name, tapeName string) *AgentNode {
	return &AgentNode{
		Kit:       k,
		AgentName: name,
		TapeName:  tapeName,
	}
}

func newWorkflowRunID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// resolveWorkflowTape substitutes workflow/{name}/default/{node} → workflow/{name}/{runID}/{node}.
func resolveWorkflowTape(base string, wctx *workflow.Context) string {
	if wctx == nil || wctx.Metadata == nil {
		return base
	}
	rid, _ := wctx.Metadata["run_id"].(string)
	rid = strings.TrimSpace(rid)
	if rid == "" {
		return base
	}
	parts := strings.Split(base, "/")
	if len(parts) >= 4 && parts[0] == "workflow" && parts[2] == "default" {
		parts[2] = rid
		return strings.Join(parts, "/")
	}
	return base + ":" + rid
}
