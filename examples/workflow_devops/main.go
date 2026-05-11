// Example: DevOps incident response using Graph + Router workflow.
//
// This demonstrates a conditional workflow that:
//  1. Classifies an alert by severity (critical / warning / info)
//  2. Routes to different handlers based on classification
//  3. Each branch uses a different system prompt and optional tools
//
// Run:
//
//	AI_API_KEY=... AI_MODEL=gpt-4o-mini go run ./examples/workflow_devops
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/workflow"
)

func main() {
	ctx := context.Background()

	// --- 1. Build devkit agent with tools ---
	opts := devkit.EnvOptions()
	if opts.APIKey == "" || opts.Model == "" {
		log.Fatal("AI_API_KEY and AI_MODEL are required")
	}
	opts.Verbose = 1
	opts.SystemPromptExtra = "Keep answers concise and actionable."

	// Register a mock tool for critical incidents.
	opts.Tools = []*tool.Tool{{
		Spec: tool.ToolSpec{
			Name:        "restart_service",
			Description: "Restart a Kubernetes deployment by name",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
					"service":   map[string]any{"type": "string", "description": "Deployment name"},
				},
				"required": []any{"namespace", "service"},
			},
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			ns := args["namespace"].(string)
			svc := args["service"].(string)
			return map[string]any{"status": "restarted", "namespace": ns, "service": svc}, nil
		},
	}}

	kit, err := devkit.Build(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = kit.Close(ctx) }()

	// --- 2. Build Graph workflow ---
	g := &workflow.Graph{Name: "incident_response"}

	// Node 1: classify severity.
	classifier := kit.AsAgentNodeWithTape("classify", "devops-classify")
	classifier.SystemPrompt = `You are an SRE classifier. Analyze the alert and reply with exactly one word:
- CRITICAL: if the service is down, data loss, or revenue impact
- WARNING: if degraded performance or elevated errors
- INFO: if routine metrics or non-actionable events
Only output one of these three words, no explanation.`
	g.AddNode("classify", classifier)

	// Branch handlers.
	critical := kit.AsAgentNodeWithTape("critical_handler", "devops-critical")
	critical.SystemPrompt = `You are an on-call SRE. The incident is CRITICAL.
1. Propose the fastest mitigation (may use restart_service tool).
2. List 3 checks to confirm recovery.
Be terse.`
	g.AddNode("critical_handler", critical)

	warning := kit.AsAgentNodeWithTape("warning_handler", "devops-warning")
	warning.SystemPrompt = `You are an SRE. The incident is WARNING.
Provide a 3-step troubleshooting checklist. Be terse.`
	g.AddNode("warning_handler", warning)

	info := kit.AsAgentNodeWithTape("info_handler", "devops-info")
	info.SystemPrompt = `You are an SRE. The incident is INFO.
Write a one-line summary for the daily standup. Be terse.`
	g.AddNode("info_handler", info)

	// Edges: START → classify → (conditional branches)
	g.AddEdge("START", "classify")

	// Conditional routing based on classify output.
	g.AddConditionalEdges("classify",
		workflow.Default(
			workflow.ContainsRouter(map[string]string{
				"critical": "CRITICAL",
				"warning":  "WARNING",
			}),
			"info",
		),
		map[string]string{
			"critical": "critical_handler",
			"warning":  "warning_handler",
			"info":     "info_handler",
		},
	)

	// --- 3. Run workflow ---
	alert := "Production API returning 502 errors for 5 minutes, CPU at 95%"
	fmt.Printf("Alert: %s\n\n", alert)

	res, err := kit.RunWorkflow(ctx, g, alert)
	if err != nil {
		log.Fatalf("workflow failed: %v", err)
	}

	fmt.Printf("Workflow completed in %d steps.\n", res.Steps)
	fmt.Printf("\nFinal output:\n%s\n", res.Output)

	// --- 4. Inspect tape history ---
	fmt.Println("\n--- Tape history ---")
	for _, tape := range []string{"devops-classify", "devops-critical", "devops-warning", "devops-info"} {
		entries, _ := kit.Store.FetchAll(tape, nil)
		if len(entries) > 0 {
			fmt.Printf("Tape %q: %d entries\n", tape, len(entries))
		}
	}
}
