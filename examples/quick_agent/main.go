// Example: minimal agent with the QuickAgent/QuickRun/QuickCrew facade.
//
// Run:
//
//	AI_API_KEY=... AI_MODEL=gpt-4o-mini go run ./examples/quick_agent
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/tool"
)

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("AI_API_KEY")
	model := os.Getenv("AI_MODEL")
	apiBase := os.Getenv("AI_API_BASE")
	if apiKey == "" {
		log.Fatal("AI_API_KEY environment variable is required")
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	// --- Demo 1: QuickAgent + QuickRun ---
	fmt.Println("== Demo 1: QuickAgent + QuickRun ==")

	agent, err := devkit.QuickAgent(ctx, devkit.QuickAgentConfig{
		Model:        model,
		APIKey:       apiKey,
		APIBase:      apiBase,
		SystemPrompt: "You are a helpful assistant. Keep answers under 3 sentences.",
	})
	if err != nil {
		log.Fatalf("QuickAgent failed: %v", err)
	}

	out, err := devkit.QuickRun(ctx, agent, "What is the capital of France?")
	if err != nil {
		log.Fatalf("QuickRun failed: %v", err)
	}
	fmt.Println(out)
	fmt.Println()

	// --- Demo 2: QuickAgent with a tool ---
	fmt.Println("== Demo 2: QuickAgent with tool ==")

	agentWithTool, err := devkit.QuickAgent(ctx, devkit.QuickAgentConfig{
		Model:   model,
		APIKey:  apiKey,
		APIBase: apiBase,
		Tools: []*tool.Tool{
			{
				Spec: tool.ToolSpec{
					Name:        "reverse",
					Description: "Reverse a string.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text": map[string]any{"type": "string"},
						},
						"required": []any{"text"},
					},
				},
				Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
					text, _ := args["text"].(string)
					runes := []rune(text)
					for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
						runes[i], runes[j] = runes[j], runes[i]
					}
					return string(runes), nil
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("QuickAgent with tool failed: %v", err)
	}

	out, err = devkit.QuickRun(ctx, agentWithTool, "Reverse the string 'hello world' using the reverse tool.")
	if err != nil {
		log.Fatalf("QuickRun failed: %v", err)
	}
	fmt.Println(out)
	fmt.Println()

	// --- Demo 3: QuickCrew ---
	fmt.Println("== Demo 3: QuickCrew (multi-agent pipeline) ==")

	crewRes, err := devkit.QuickCrew(ctx, devkit.QuickCrewConfig{
		Agents: []devkit.QuickAgentConfig{
			{
				Model:        model,
				APIKey:       apiKey,
				APIBase:      apiBase,
				SystemPrompt: "You are a researcher. Produce a short bullet list of facts.",
				MaxSteps:     3,
			},
			{
				Model:        model,
				APIKey:       apiKey,
				APIBase:      apiBase,
				SystemPrompt: "You are a summarizer. Summarize the research into one sentence.",
				MaxSteps:     3,
			},
		},
	}, "Tell me about the Go programming language.")
	if err != nil {
		log.Fatalf("QuickCrew failed: %v", err)
	}
	fmt.Println(crewRes.Output)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
