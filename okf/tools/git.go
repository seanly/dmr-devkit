package tools

import (
	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/okf/service"
)

// GitReadToolsWith returns the read-only git versioning tools (okfGitLog,
// okfGitDiff) over a Resolver. Unlike the single-bundle GitReadTools wrapper,
// these are always constructed; a call against a service with no VCS attached
// returns the service's errVCSDisabled error at call time.
func GitReadToolsWith(r Resolver) []*tool.Tool {
	return []*tool.Tool{
		gitLogTool(r),
		gitDiffTool(r),
	}
}

// GitWriteToolsWith returns the write git versioning tools (okfCommit, okfGitRevert,
// okfGitRestore) over a Resolver. As with GitReadToolsWith, VCS is checked per
// call.
func GitWriteToolsWith(r Resolver) []*tool.Tool {
	return []*tool.Tool{
		commitTool(r),
		gitRevertTool(r),
		gitRestoreTool(r),
	}
}

// GitReadTools / GitWriteTools are single-bundle back-compat wrappers that
// gate registration on VCS being attached (so a no-git playground session
// never lists git tools). When VCS is present they delegate to the With
// variants.
func GitReadTools(svc *service.Service) []*tool.Tool {
	if svc.VCS() == nil {
		return nil
	}
	return GitReadToolsWith(SingleResolver(svc))
}

func GitWriteTools(svc *service.Service) []*tool.Tool {
	if svc.VCS() == nil {
		return nil
	}
	return GitWriteToolsWith(SingleResolver(svc))
}

func gitLogTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGitLog",
			Description: "Show commit history of the bundle, optionally filtered to one concept. Each entry lists sha, author, time, message, and changed files. Use to audit what the agent changed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"concept_id": map[string]any{"type": "string", "description": "Optional concept id to filter history (e.g. \"tables/orders\")"},
					"limit":      map[string]any{"type": "integer", "description": "Max entries (default 50)"},
					"bundle":     bundleParam,
				},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGitLog, git_log, git, log, history, commit",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			id, _ := stringParam(args, "concept_id")
			limit := intArg(args, "limit")
			return svc.GitLog(id, limit)
		},
	}
}

func gitDiffTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGitDiff",
			Description: "Show a textual diff. With no ref, shows uncommitted working-tree changes; with a ref (commit sha), shows what that commit changed. Optionally restrict to one concept.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"concept_id": map[string]any{"type": "string", "description": "Optional concept id to restrict the diff"},
					"ref":        map[string]any{"type": "string", "description": "Commit sha; empty = uncommitted changes"},
					"bundle":     bundleParam,
				},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGitDiff, git_diff, git, diff, compare",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			id, _ := stringParam(args, "concept_id")
			ref, _ := stringParam(args, "ref")
			diff, err := svc.GitDiff(id, ref)
			if err != nil {
				return nil, err
			}
			return map[string]any{"diff": diff}, nil
		},
	}
}

func commitTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfCommit",
			Description: "Stage all pending bundle changes and create a git commit. No-op (committed=false) if the tree is clean. Call after meaningful edits so okfGitRevert/okfGitRestore have a baseline.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string", "description": "Commit message describing the change"},
					"bundle":  bundleParam,
				},
				"required": []string{"message"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfCommit, commit, git, snapshot, save",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			msg, err := requiredString(args, "message")
			if err != nil {
				return nil, err
			}
			return svc.Commit(msg)
		},
	}
}

func gitRevertTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGitRevert",
			Description: "Undo a commit by creating a new reverse commit (history is preserved). Use to roll back a bad agent edit. The in-memory bundle and search index are resynced automatically.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref":    map[string]any{"type": "string", "description": "Commit sha to revert (from okfGitLog)"},
					"bundle": bundleParam,
				},
				"required": []string{"ref"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGitRevert, git_revert, git, revert, undo, rollback",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			ref, err := requiredString(args, "ref")
			if err != nil {
				return nil, err
			}
			return svc.Revert(ref)
		},
	}
}

func gitRestoreTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGitRestore",
			Description: "Restore a single concept to its state at a given commit (does not rewrite history). Use to recover one concept without reverting a whole commit.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"concept_id": map[string]any{"type": "string", "description": "Concept id to restore (e.g. \"tables/orders\")"},
					"ref":        map[string]any{"type": "string", "description": "Commit sha to restore from (from okfGitLog)"},
					"bundle":     bundleParam,
				},
				"required": []string{"concept_id", "ref"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGitRestore, git_restore, git, restore, recover",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			id, err := requiredString(args, "concept_id")
			if err != nil {
				return nil, err
			}
			ref, err := requiredString(args, "ref")
			if err != nil {
				return nil, err
			}
			return svc.Restore(id, ref)
		},
	}
}

// intArg extracts an integer from tool args, tolerating the float64 encoding
// JSON unmarshalling produces. Returns 0 when absent or non-numeric.
func intArg(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
