package vision

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/seanly/dmr-devkit/provider"
	openaipkg "github.com/seanly/dmr-devkit/provider/openai"
	"github.com/seanly/dmr-devkit/tool"
)

// Config configures the vision analysis tool.
type Config struct {
	Model   string
	APIKey  string
	APIBase string
	Headers map[string]string
}

// NewAnalyzeTool creates a vision analysis tool that calls a vision-capable model.
// It is self-contained: it creates its own OpenAI-compatible client and does not
// depend on the agent's core LLM or tool loop.
func NewAnalyzeTool(cfg Config) *tool.Tool {
	oc := openaipkg.ClientConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.APIBase,
		Headers: cfg.Headers,
	}
	client := openaipkg.NewClient(oc)

	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "visionAnalyze",
			Description: "Analyze an image file and return a text description of its content. Call this when you need to understand an image referenced by the user.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Absolute path to the image file",
					},
					"question": map[string]any{
						"type":        "string",
						"description": "Optional: what to look for in the image (default: describe the image in detail)",
					},
				},
				"required": []any{"path"},
			},
			Group:      tool.ToolGroupCore,
			AlwaysLoad: true,
		},
		Handler: func(ctx *tool.ToolContext, args map[string]any) (any, error) {
			path, _ := args["path"].(string)
			question, _ := args["question"].(string)
			if question == "" {
				question = "Describe this image in detail"
			}
			if path == "" {
				return nil, fmt.Errorf("path is required")
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read image file: %w", err)
			}

			mime := detectMIME(path, data)
			b64 := base64.StdEncoding.EncodeToString(data)
			imageURL := fmt.Sprintf("data:%s;base64,%s", mime, b64)

			mcpCtx := context.Background()
			if ctx != nil && ctx.Ctx != nil {
				mcpCtx = ctx.Ctx
			}

			req := provider.ChatRequest{
				Model: cfg.Model,
				Messages: []provider.Message{
					{
						Role: "user",
						Parts: []provider.ContentPart{
							provider.TextPart{Text: question},
							provider.ImagePart{URL: imageURL},
						},
					},
				},
			}

			resp, err := client.ChatCompletion(mcpCtx, req)
			if err != nil {
				return nil, fmt.Errorf("vision model call failed: %w", err)
			}
			if resp == nil {
				return nil, fmt.Errorf("vision model returned nil response")
			}
			return resp.Text, nil
		},
	}
}

func detectMIME(path string, data []byte) string {
	switch {
	case len(data) > 6 && string(data[:6]) == "\x89PNG\r\n":
		return "image/png"
	case len(data) > 2 && string(data[:2]) == "\xff\xd8":
		return "image/jpeg"
	case len(data) > 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case strings.HasSuffix(strings.ToLower(path), ".png"):
		return "image/png"
	case strings.HasSuffix(strings.ToLower(path), ".gif"):
		return "image/gif"
	case strings.HasSuffix(strings.ToLower(path), ".webp"):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
