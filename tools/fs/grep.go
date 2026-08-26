package fs

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/core"
	"github.com/seanly/dmr-devkit/tool"
)

func grepTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "fsGrep",
			Description: "Search file contents with regex (ripgrep). Default output_mode returns matching file paths only; use content mode for matching lines with optional context. Respects .gitignore.",
			Group:       tool.ToolGroupCore,
			SearchHint:  "grep, search, find, ripgrep, rg, pattern, regex, explore, codebase",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Regular expression pattern to search for"},
					"path":    map[string]any{"type": "string", "description": "Directory or file to search (default: workspace root)"},
					"glob":    map[string]any{"type": "string", "description": "File glob filter (e.g. \"*.go\", \"**/*.ts\")"},
					"output_mode": map[string]any{
						"type":        "string",
						"default":     "files_with_matches",
						"description": "files_with_matches: paths only; content: matching lines; count: match counts per file",
						"enum":        []string{"files_with_matches", "content", "count"},
					},
					"head_limit":     map[string]any{"type": "integer", "default": defaultHeadLimit, "description": "Max results to return"},
					"offset":         map[string]any{"type": "integer", "default": 0, "description": "Skip first N results (pagination)"},
					"context":        map[string]any{"type": "integer", "description": "Lines of context before and after each match (-C)"},
					"before":         map[string]any{"type": "integer", "description": "Lines of context before each match (-B)"},
					"after":          map[string]any{"type": "integer", "description": "Lines of context after each match (-A)"},
					"case_sensitive": map[string]any{"type": "boolean", "default": false, "description": "Case-sensitive search"},
					"multiline":      map[string]any{"type": "boolean", "default": false, "description": "Enable multiline matching (-U --multiline-dotall)"},
				},
				"required": []string{"pattern"},
			},
		},
		Handler:     grepHandler,
		NeedContext: true,
	}
}

func grepHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	pattern := stringArg(args, "pattern")
	if pattern == "" {
		return nil, core.MakeErrToolExec("fsGrep", fmt.Errorf("pattern is required and must be a string")).Build()
	}

	searchPath, err := resolveSearchPath(ctx, stringArg(args, "path"), true)
	if err != nil {
		return nil, core.FromError(err).With("tool", "fsGrep").Build()
	}

	mode := grepOutputMode(stringArg(args, "output_mode"))
	if mode == "" {
		mode = grepModeFiles
	}
	switch mode {
	case grepModeFiles, grepModeContent, grepModeCount:
	default:
		return nil, core.MakeErrToolExec("fsGrep", fmt.Errorf("output_mode must be files_with_matches, content, or count")).Build()
	}

	headLimit := intArg(args, "head_limit", defaultHeadLimit)
	if headLimit <= 0 {
		headLimit = defaultHeadLimit
	}

	opts := grepOptions{
		pattern:       pattern,
		searchPath:    searchPath,
		glob:          stringArg(args, "glob"),
		outputMode:    mode,
		headLimit:     headLimit,
		offset:        intArg(args, "offset", 0),
		contextLines:  intArg(args, "context", 0),
		beforeLines:   intArg(args, "before", 0),
		afterLines:    intArg(args, "after", 0),
		caseSensitive: boolArg(args, "case_sensitive"),
		multiline:     boolArg(args, "multiline"),
	}

	runCtx := ctx.Ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	return runGrep(runCtx, opts)
}
