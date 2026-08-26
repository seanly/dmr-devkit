package fs

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tool"
)

func globTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "fsGlob",
			Description: "Find files by glob pattern (ripgrep --files). Respects .gitignore. Example patterns: \"**/*.go\", \"plugins/**/*.ts\".",
			Group:       tool.ToolGroupCore,
			SearchHint:  "glob, find files, list files, pattern, search, explore, codebase",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":    map[string]any{"type": "string", "description": "Glob pattern (e.g. \"**/*.go\")"},
					"path":       map[string]any{"type": "string", "description": "Search root directory (default: workspace root)"},
					"head_limit": map[string]any{"type": "integer", "default": defaultHeadLimit, "description": "Max files to return"},
					"offset":     map[string]any{"type": "integer", "default": 0, "description": "Skip first N results (pagination)"},
				},
				"required": []string{"pattern"},
			},
		},
		Handler:     globHandler,
		NeedContext: true,
	}
}

func globHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	pattern := stringArg(args, "pattern")
	if pattern == "" {
		return nil, core.MakeErrToolExec("fsGlob", fmt.Errorf("pattern is required and must be a string")).Build()
	}

	searchPath, err := resolveSearchPath(ctx, stringArg(args, "path"), true)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsGlob").Build()
	}

	headLimit := intArg(args, "head_limit", defaultHeadLimit)
	if headLimit <= 0 {
		headLimit = defaultHeadLimit
	}

	opts := globOptions{
		pattern:    pattern,
		searchPath: searchPath,
		headLimit:  headLimit,
		offset:     intArg(args, "offset", 0),
	}

	runCtx := ctx.Ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	return runGlob(runCtx, opts)
}
