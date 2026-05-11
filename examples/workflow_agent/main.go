// Example: orchestrate multiple agent steps using pkg/workflow with devkit.
//
// This demonstrates a Sequential workflow that:
//  1. Brainstorms ideas
//  2. Drafts content based on the brainstorm
//  3. Summarizes the draft
//
// Run:
//
//	AI_API_KEY=... AI_MODEL=gpt-4o-mini go run ./examples/workflow_agent
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/workflow"
)

func main() {
	ctx := context.Background()

	// --- 1. Build devkit agent ---
	opts := devkit.EnvOptions()
	if opts.APIKey == "" || opts.Model == "" {
		log.Fatal("AI_API_KEY and AI_MODEL are required")
	}
	opts.Verbose = 1
	opts.SystemPromptExtra = "Keep answers concise."

	kit, err := devkit.Build(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = kit.Close(ctx) }()

	// --- 2. Define workflow nodes ---
	// Each node wraps the same agent but uses a dedicated tape so that the
	// conversation history is isolated per step.

	brainstorm := kit.AsAgentNodeWithTape("brainstorm", "wf-brainstorm")
	brainstorm.SystemPrompt = "You are a creative brainstormer. Generate 3 short bullet ideas for the given topic."

	drafter := kit.AsAgentNodeWithTape("drafter", "wf-drafter")
	drafter.SystemPrompt = "You are a writer. Expand the provided ideas into a short paragraph."

	summarizer := kit.AsAgentNodeWithTape("summarizer", "wf-summarizer")
	summarizer.SystemPrompt = "You are an editor. Summarize the provided text in one sentence."

	// --- 3. Assemble Sequential workflow ---
	seq := &workflow.Sequential{
		WorkflowName: "content_pipeline",
		Nodes:        []workflow.Node{brainstorm, drafter, summarizer},
	}

	// --- 4. Run workflow ---
	topic := "The benefits of morning exercise"
	fmt.Printf("Topic: %s\n\n", topic)

	res, err := kit.RunWorkflow(ctx, seq, topic)
	if err != nil {
		log.Fatalf("workflow failed: %v", err)
	}

	fmt.Printf("Workflow completed in %d steps.\n", res.Steps)
	fmt.Printf("\nFinal output:\n%s\n", res.Output)

	// --- 5. (Optional) Inspect tape history ---
	fmt.Println("\n--- Tape history ---")
	for _, tape := range []string{"wf-brainstorm", "wf-drafter", "wf-summarizer"} {
		entries, _ := kit.Store.FetchAll(tape, nil)
		fmt.Printf("Tape %q: %d entries\n", tape, len(entries))
	}

	// --- 6. Demonstrate Parallel workflow ---
	fmt.Println("\n--- Parallel workflow demo ---")
	parallel := &workflow.Parallel{
		WorkflowName: "parallel_research",
		Nodes: []workflow.Node{
			kit.AsAgentNodeWithTape("researcher_a", "wf-par-a"),
			kit.AsAgentNodeWithTape("researcher_b", "wf-par-b"),
		},
	}
	// Override prompts via SystemPrompt for variety.
	parallel.Nodes[0].(*devkit.AgentNode).SystemPrompt = "List 3 pros of remote work."
	parallel.Nodes[1].(*devkit.AgentNode).SystemPrompt = "List 3 cons of remote work."

	parRes, err := kit.RunWorkflow(ctx, parallel, "remote work")
	if err != nil {
		log.Fatalf("parallel workflow failed: %v", err)
	}

	fmt.Printf("Parallel workflow completed in %d steps.\n", parRes.Steps)
	for i, out := range parRes.Output.([]any) {
		fmt.Printf("\nBranch %d output:\n%s\n", i+1, out)
	}
}
