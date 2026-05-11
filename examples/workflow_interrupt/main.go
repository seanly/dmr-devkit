// Example: Human-in-the-Loop workflow with interrupt and resume.
//
// This demonstrates a Sequential workflow that pauses at an approval node
// and waits for external human input before continuing.
//
// Scenario:
//  1. Writer generates a draft.
//  2. Approval node interrupts and surfaces the draft to a human reviewer.
//  3. After the human clicks "approve", the workflow resumes and publishes.
//
// Run:
//
//	go run ./examples/workflow_interrupt
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/seanly/dmr-devkit/workflow"
)

// promptForReview shows the interrupt payload and waits for user input
// from stdin. Valid responses: "approve" or "reject".
//
// In a real application you might:
//   - Start an HTTP server and wait for a POST /resume callback
//   - Listen on a message queue (e.g. RabbitMQ, Kafka)
//   - Block on a WebSocket / SSE channel
//   - Poll a database row for status changes
func promptForReview(value any) string {
	payload, _ := value.(map[string]any)
	fmt.Println("\n========== HUMAN REVIEW REQUIRED ==========")
	fmt.Printf("Draft: %s\n", payload["draft"])
	fmt.Printf("Topic: %s\n", payload["topic"])
	fmt.Println("===========================================")
	fmt.Println()

	for {
		fmt.Print("Enter decision (approve / reject): ")
		var decision string
		if _, err := fmt.Scanln(&decision); err != nil {
			fmt.Println("Failed to read input, please try again.")
			continue
		}
		decision = strings.TrimSpace(strings.ToLower(decision))
		if decision == "approve" || decision == "reject" {
			return decision
		}
		fmt.Println("Invalid input. Please type 'approve' or 'reject'.")
	}
}

func main() {
	ctx := context.Background()

	// --- 1. Define workflow nodes ---

	writer := workflow.NodeFunc{
		N: "writer",
		F: func(_ context.Context, _ *workflow.Context, input any) (any, error) {
			topic := input.(string)
			// In a real app this would call an LLM. Here we simulate output.
			draft := fmt.Sprintf("Draft about %q: Morning exercise boosts energy and mood.", topic)
			return draft, nil
		},
	}

	approval := workflow.NodeFunc{
		N: "approval",
		F: func(_ context.Context, wctx *workflow.Context, input any) (any, error) {
			draft := input.(string)
			// Interrupt surfaces the draft to an external reviewer.
			// On resume, this returns the reviewer's decision.
			decision, err := workflow.Interrupt(wctx, map[string]any{
				"type":  "approval",
				"draft": draft,
				"topic": wctx.State["topic"],
			})
			if err != nil {
				return nil, err
			}
			if decision == "approve" {
				return draft + " [APPROVED]", nil
			}
			return nil, fmt.Errorf("rejected by human reviewer")
		},
	}

	publisher := workflow.NodeFunc{
		N: "publisher",
		F: func(_ context.Context, _ *workflow.Context, input any) (any, error) {
			return "PUBLISHED: " + input.(string), nil
		},
	}

	seq := &workflow.Sequential{
		WorkflowName: "content_with_approval",
		Nodes:        []workflow.Node{writer, approval, publisher},
	}

	// --- 2. First run: executes writer, then interrupts at approval ---

	topic := "morning exercise"
	wctx := workflow.NewContext()
	wctx.SetState("topic", topic)

	fmt.Println("=== First run ===")
	res, err := seq.Run(ctx, wctx, topic)
	if err == nil {
		log.Fatal("expected interrupt error on first run")
	}

	var ie *workflow.InterruptError
	if !errors.As(err, &ie) {
		log.Fatalf("unexpected error: %v", err)
	}

	// Pretty-print the interrupt payload.
	payloadJSON, _ := json.MarshalIndent(ie.Value, "", "  ")
	fmt.Printf("Workflow interrupted. Payload:\n%s\n", payloadJSON)

	// --- 3. Persist context (simulate database save/load) ---
	// In production: serialize wctx.StepLog and wctx.State to a database,
	// then load them back when the user submits their decision.
	savedWctx := wctx.WithMetadata(nil)
	savedWctx.ResumeData = nil // ResumeData is injected on resume, not persisted

	// --- 4. Human review ---
	decision := promptForReview(ie.Value)

	// --- 5. Resume: re-run the workflow with the human's decision ---

	fmt.Println("\n=== Resume run ===")
	savedWctx.ResumeData = decision
	savedWctx.Step = 0 // reset step counter so runNode can replay from StepLog

	res, err = seq.Run(ctx, savedWctx, topic)
	if err != nil {
		log.Fatalf("workflow failed on resume: %v", err)
	}

	finalRes := res.(*workflow.Result)
	fmt.Printf("Workflow completed in %d steps.\n", finalRes.Steps)
	fmt.Printf("Final output: %s\n", finalRes.Output)

	// --- 6. Show StepLog ---
	fmt.Println("\n--- StepLog ---")
	for _, e := range savedWctx.StepLog {
		status := "OK"
		if e.Interrupted {
			status = "INTERRUPT"
		} else if e.Error != "" {
			status = "ERROR"
		}
		fmt.Printf("  step=%d node=%q status=%s\n", e.Step, e.Node, status)
	}

	// --- 7. Demonstrate reject path (re-run fresh) ---
	fmt.Println("\n=== Reject path demo ===")
	wctx2 := workflow.NewContext()
	wctx2.SetState("topic", topic)
	_, _ = seq.Run(ctx, wctx2, topic)
	// Simulate reject decision.
	wctx2.ResumeData = "reject"
	wctx2.Step = 0
	res2, err2 := seq.Run(ctx, wctx2, topic)
	if err2 != nil {
		fmt.Printf("Workflow failed as expected: %v\n", err2)
	} else {
		fmt.Printf("Unexpected success: %v\n", res2.(*workflow.Result).Output)
	}
}
