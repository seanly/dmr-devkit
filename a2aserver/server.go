// Package a2aserver exposes a DMR agent as an A2A (Agent2Agent) JSON-RPC service using a2a-go's server stack.
//
// Clients (including the DMR a2a plugin) discover the agent via the well-known agent card
// and call SendMessage on the advertised invoke URL.
package a2aserver

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/seanly/dmr-devkit/agent"
)

// WellKnownAgentCardPath is the standard path for the agent card (same as [a2asrv.WellKnownAgentCardPath]).
const WellKnownAgentCardPath = a2asrv.WellKnownAgentCardPath

// Runner runs one agent turn. [*agent.Agent] satisfies this interface.
type Runner interface {
	Run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32) (*agent.Result, error)
}

// Options configures routes and the agent card. PublicInvokeURL must be the absolute URL clients use for JSON-RPC POST (same path as MountPath on your public host).
type Options struct {
	AgentName          string
	Description        string
	PublicInvokeURL    string   // e.g. https://example.com/a2a/invoke
	MountPath          string   // e.g. /a2a/invoke (default "/invoke")
	TapeMode           TapeMode // default [TapeModeAuto]: tape per A2A TaskID; [TapeModeFixed] uses TapeName.
	TapePrefix         string   // auto mode: prefix for flat tape name (default "a2a"); see [AutoTapeName].
	TapeName           string   // fixed mode only: constant tape passed to Runner.Run (default "default").
	DefaultInputModes  []string
	DefaultOutputModes []string

	// ChunkSize controls streaming artifact output. If > 0, the agent output is split
	// into chunks of this size and delivered as a sequence of TaskArtifactUpdateEvent
	// via SSE streaming. If 0 (default), the entire output is returned as a single
	// Message (non-streaming behavior).
	ChunkSize int
}

func (o *Options) applyDefaults() error {
	if o.MountPath == "" {
		o.MountPath = "/invoke"
	}
	if !strings.HasPrefix(o.MountPath, "/") {
		o.MountPath = "/" + o.MountPath
	}
	if len(o.DefaultInputModes) == 0 {
		o.DefaultInputModes = []string{"text"}
	}
	if len(o.DefaultOutputModes) == 0 {
		o.DefaultOutputModes = []string{"text"}
	}
	if o.TapeMode == "" {
		o.TapeMode = TapeModeAuto
	}
	switch o.TapeMode {
	case TapeModeFixed:
		if o.TapeName == "" {
			o.TapeName = "default"
		}
	case TapeModeAuto:
		if o.TapePrefix == "" {
			o.TapePrefix = "a2a"
		}
	default:
		return fmt.Errorf("a2aserver: invalid TapeMode %q (use %q or %q)", o.TapeMode, TapeModeAuto, TapeModeFixed)
	}
	if o.AgentName == "" {
		o.AgentName = "dmr"
	}
	return nil
}

// Mount registers the well-known agent card and JSON-RPC handler on mux.
func Mount(mux *http.ServeMux, opts Options, runner Runner) error {
	if err := opts.applyDefaults(); err != nil {
		return err
	}
	if opts.PublicInvokeURL == "" {
		return fmt.Errorf("a2aserver: PublicInvokeURL is required")
	}
	if runner == nil {
		return fmt.Errorf("a2aserver: Runner is required")
	}

	card := &a2a.AgentCard{
		Name:        opts.AgentName,
		Description: opts.Description,
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(opts.PublicInvokeURL, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  opts.DefaultInputModes,
		DefaultOutputModes: opts.DefaultOutputModes,
	}

	h := a2asrv.NewHandler(&executor{runner: runner, opts: opts})
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle(opts.MountPath, a2asrv.NewJSONRPCHandler(h))
	return nil
}

type executor struct {
	runner Runner
	opts   Options
}

func (e *executor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.Message == nil {
			_ = yield(nil, fmt.Errorf("a2aserver: missing message"))
			return
		}
		prompt := messageUserText(execCtx.Message)
		if strings.TrimSpace(prompt) == "" {
			_ = yield(nil, fmt.Errorf("a2aserver: empty user message"))
			return
		}
		res, err := e.runner.Run(ctx, e.opts.resolveTape(execCtx), prompt, 0)
		if err != nil {
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Error: "+err.Error()))
			_ = yield(msg, nil)
			return
		}

		output := res.Output
		if e.opts.ChunkSize <= 0 || len(output) <= e.opts.ChunkSize {
			// Non-streaming: single Message delivery.
			msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(output))
			_ = yield(msg, nil)
			return
		}

		// Streaming artifact mode: split large output into chunks.
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}

		chunkSize := e.opts.ChunkSize
		var artifactID a2a.ArtifactID
		for i := 0; i < len(output); i += chunkSize {
			end := i + chunkSize
			if end > len(output) {
				end = len(output)
			}
			chunk := output[i:end]

			var event *a2a.TaskArtifactUpdateEvent
			if artifactID == "" {
				event = a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(chunk))
				artifactID = event.Artifact.ID
			} else {
				event = a2a.NewArtifactUpdateEvent(execCtx, artifactID, a2a.NewTextPart(chunk))
			}
			if end >= len(output) {
				event.LastChunk = true
			}
			if !yield(event, nil) {
				return
			}
		}

		_ = yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

func (e *executor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		_ = yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

func messageUserText(m *a2a.Message) string {
	return partsToString(m.Parts)
}

func partsToString(parts a2a.ContentParts) string {
	var b strings.Builder
	for _, p := range parts {
		if t := p.Text(); t != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(t)
			continue
		}
		if raw := p.Raw(); len(raw) > 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(string(raw))
			continue
		}
		if d := p.Data(); d != nil {
			raw, err := json.Marshal(d)
			if err != nil {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(string(raw))
		}
	}
	return b.String()
}
