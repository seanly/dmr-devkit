// Example: minimal multi-turn agent with tools using pkg/devkit (no TOML / CLI).
//
// Run:
//
//	AI_API_KEY=... AI_MODEL=gpt-4o-mini go run ./examples/devkit_agent
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/tool"
)

func main() {
	ctx := context.Background()

	opts := devkit.EnvOptions()
	if opts.APIKey == "" || opts.Model == "" {
		log.Fatal("AI_API_KEY and AI_MODEL are required")
	}
	opts.Verbose = 1
	opts.SystemPromptExtra = "Keep answers concise."
	opts.Tools = []*tool.Tool{
		{
			Spec: tool.ToolSpec{
				Name:        "echo",
				Description: "Echo the message back to the user.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"message": map[string]any{"type": "string", "description": "Text to echo"},
					},
					"required": []any{"message"},
				},
			},
			Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
				msg, _ := args["message"].(string)
				return map[string]any{"echo": msg}, nil
			},
		},
	}

	kit, err := devkit.Build(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = kit.Close(ctx) }()

	prompt := "Call the echo tool once with message \"hello from devkit\" and summarize what it returned."
	res, err := kit.Agent.Run(ctx, devkit.DefaultTapeName, prompt, 0)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Output)
}
