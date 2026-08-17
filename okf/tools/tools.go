// Package tools wires a *service.Service into dmr *tool.Tool constructors.
//
// It is the single source of truth for the OKF agent tool surface. Both
// okf-devkit's playground and dmr's okf plugin import these constructors so
// the two surfaces never drift.
//
// Multi-bundle routing: constructors take a Resolver (bundle name →
// *service.Service) rather than a single Service. Every tool exposes an
// optional "bundle" parameter; the handler resolves the target service per
// call (empty bundle = the default bundle). Single-bundle callers use
// SingleResolver and the ReadTools/WriteTools convenience wrappers.
//
// Aggregates:
//   - ReadToolsWith(r)  — non-mutating tools (consumer + dev + okfPresentInvestigation)
//   - WriteToolsWith(r) — mutating producer tools (okfCreateConcept / okfUpdateConcept / okfSetFrontmatter / okfAppendSection / okfDeleteConcept)
//   - GitReadToolsWith(r) / GitWriteToolsWith(r) — git versioning tools (appended by callers that enable git)
//   - ReadTools(svc) / WriteTools(svc) — single-bundle back-compat wrappers (git gated by VCS at registration)
//
// Source fetchers (fetch_url / read_file / list_dir) are intentionally NOT
// here: they are generic, non-svc-backed helpers owned by okf-devkit's
// playground. dmr exposes equivalents via its fs / webtool plugins.
package tools

import (
	"fmt"

	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/okf/service"
)

// Resolver maps a bundle name ("" = the default bundle) to the Service that
// backs it. Returning an error lets a tool fail gracefully when the caller
// names an unknown bundle.
type Resolver func(bundle string) (*service.Service, error)

// SingleResolver returns a Resolver that always answers with svc regardless of
// the requested bundle name. Used by single-bundle callers (the playground, the
// okf plugin in single-bundle mode).
func SingleResolver(svc *service.Service) Resolver {
	return func(string) (*service.Service, error) { return svc, nil }
}

// bundleParam is the optional "bundle" selector added to every tool spec so the
// agent can scope a call to a specific bundle in multi-bundle mode.
var bundleParam = map[string]any{
	"type":        "string",
	"description": "Bundle name to operate on (optional; defaults to the default bundle). Use okfListBundles to enumerate.",
}

// bundleArg extracts the optional "bundle" selector from tool args.
func bundleArg(args map[string]any) string {
	b, _ := args["bundle"].(string)
	return b
}

// resolveSvc is the per-call service lookup shared by every handler.
func resolveSvc(r Resolver, args map[string]any) (*service.Service, error) {
	return r(bundleArg(args))
}

// ReadTools returns the non-mutating OKF tools for a single-bundle caller: the
// 8 consumer tools, the development/validation tools, okfPresentInvestigation,
// and the read-only git tools when VCS is attached. (Multi-bundle callers use
// ReadToolsWith and append GitReadToolsWith themselves.)
func ReadTools(svc *service.Service) []*tool.Tool {
	tools := ReadToolsWith(SingleResolver(svc))
	tools = append(tools, GitReadTools(svc)...)
	return tools
}

// WriteTools returns the mutating OKF tools for a single-bundle caller: the 5
// producer tools plus the git write tools when VCS is attached.
func WriteTools(svc *service.Service) []*tool.Tool {
	tools := WriteToolsWith(SingleResolver(svc))
	tools = append(tools, GitWriteTools(svc)...)
	return tools
}

// ReadToolsWith returns the non-mutating tools (consumer + dev +
// okfPresentInvestigation) over a Resolver. Git read tools are NOT included;
// callers that enable git append GitReadToolsWith(r).
func ReadToolsWith(r Resolver) []*tool.Tool {
	tools := append(ConsumerToolsWith(r), DevToolsWith(r)...)
	tools = append(tools, presentInvestigationTool())
	return tools
}

// WriteToolsWith returns the 5 producer (mutation) tools over a Resolver. Git
// write tools are NOT included; callers that enable git append
// GitWriteToolsWith(r).
func WriteToolsWith(r Resolver) []*tool.Tool {
	return ProducerToolsWith(r)
}

// ConsumerToolsWith returns the 8 read-only consumer tools (the MCP surface)
// over a Resolver.
func ConsumerToolsWith(r Resolver) []*tool.Tool {
	return []*tool.Tool{
		listConceptsTool(r),
		searchConceptsTool(r),
		getConceptTool(r),
		getIndexTool(r),
		getNeighborsTool(r),
		getBacklinksTool(r),
		checkStaleTool(r),
		getTrustedTool(r),
	}
}

// DevToolsWith returns the development / validation tools over a Resolver.
func DevToolsWith(r Resolver) []*tool.Tool {
	return []*tool.Tool{
		validateBundleTool(r),
		bundleStatsTool(r),
		listTypesTool(r),
		getGraphTool(r),
		getGraphSummaryTool(r),
	}
}

// ConsumerTools / DevTools / ProducerTools are single-bundle back-compat
// wrappers for callers that hold one *service.Service.
func ConsumerTools(svc *service.Service) []*tool.Tool { return ConsumerToolsWith(SingleResolver(svc)) }
func DevTools(svc *service.Service) []*tool.Tool      { return DevToolsWith(SingleResolver(svc)) }

// --- helpers ---

func stringParam(args map[string]any, key string) (string, error) {
	v, _ := args[key].(string)
	return v, nil
}

func requiredString(args map[string]any, key string) (string, error) {
	v, _ := args[key].(string)
	if v == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return v, nil
}

// --- consumer tools ---

func listConceptsTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfListConcepts",
			Description: "List all concepts in the bundle. Optionally filter by type or tag.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":   map[string]any{"type": "string", "description": "Filter by concept type (e.g. table, guideline, attested_computation)"},
					"tag":    map[string]any{"type": "string", "description": "Filter by tag"},
					"bundle": bundleParam,
				},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfListConcepts, list_concepts, concept, list, enumerate, type, tag",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			typ, _ := stringParam(args, "type")
			tag, _ := stringParam(args, "tag")
			return svc.ListConcepts(typ, tag), nil
		},
	}
}

func searchConceptsTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfSearchConcepts",
			Description: "Search for concepts in the OKF bundle by keywords matching titles, types, tags, and body content.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":  map[string]any{"type": "string", "description": "Search query string"},
					"bundle": bundleParam,
				},
				"required": []string{"query"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfSearchConcepts, search_concepts, concept, search, query, fts, full text",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			query, err := requiredString(args, "query")
			if err != nil {
				return nil, err
			}
			return svc.SearchConcepts(query), nil
		},
	}
}

func getConceptTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGetConcept",
			Description: "Get full details of a concept by its ID (progressive disclosure).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "The concept ID (bundle-relative path without .md)"},
					"bundle": bundleParam,
				},
				"required": []string{"id"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGetConcept, get_concept, concept, get, detail, id, progressive disclosure",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			id, err := requiredString(args, "id")
			if err != nil {
				return nil, err
			}
			return svc.GetConcept(id)
		},
	}
}

func getIndexTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGetIndex",
			Description: "Return the navigation structure of an index.md (root by default, or a subdirectory).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "Subdirectory path for a directory-local index.md (default: root)"},
					"bundle": bundleParam,
				},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGetIndex, get_index, index, navigation, directory, toc",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			path, _ := stringParam(args, "path")
			return svc.GetIndex(path)
		},
	}
}

func getNeighborsTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGetNeighbors",
			Description: "Get the concepts directly referenced by a concept (forward graph traversal).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "The concept ID"},
					"bundle": bundleParam,
				},
				"required": []string{"id"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGetNeighbors, get_neighbors, neighbor, graph, forward, link, reference",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			id, err := requiredString(args, "id")
			if err != nil {
				return nil, err
			}
			return svc.GetNeighbors(id)
		},
	}
}

func getBacklinksTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGetBacklinks",
			Description: "Get the concepts that reference a concept (reverse traversal / impact analysis).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "The concept ID"},
					"bundle": bundleParam,
				},
				"required": []string{"id"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGetBacklinks, get_backlinks, backlink, reverse, impact, reference",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			id, err := requiredString(args, "id")
			if err != nil {
				return nil, err
			}
			return svc.GetBacklinks(id)
		},
	}
}

func checkStaleTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfCheckStale",
			Description: "List concepts that are stale (past stale_after) or deprecated.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"bundle": bundleParam},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfCheckStale, check_stale, stale, deprecated, expired, status",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			return svc.CheckStale(), nil
		},
	}
}

func getTrustedTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGetTrusted",
			Description: "List concepts filtered by trust tier (default: human-reviewed).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tier":   map[string]any{"type": "string", "description": "Trust tier: human-reviewed|machine-confirmed|unverified (default human-reviewed)"},
					"bundle": bundleParam,
				},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGetTrusted, get_trusted, trust, tier, verified, human-reviewed",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			tier, _ := stringParam(args, "tier")
			return svc.GetTrusted(tier)
		},
	}
}

// --- development / validation tools ---

func validateBundleTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfValidateBundle",
			Description: "Run OKF compliance validation and return issues.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"bundle": bundleParam},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfValidateBundle, validate_bundle, validate, compliance, spec, lint",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			return svc.ValidateBundle(), nil
		},
	}
}

func bundleStatsTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfBundleStats",
			Description: "Return bundle statistics: concept count, types, trust tiers, link counts.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"bundle": bundleParam},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfBundleStats, bundle_stats, stats, count, statistics, summary",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			return svc.BundleStats(), nil
		},
	}
}

func listTypesTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfListTypes",
			Description: "Return all unique concept types and their counts.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"bundle": bundleParam},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfListTypes, list_types, type, count, taxonomy",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			return svc.ListTypes(), nil
		},
	}
}

func getGraphTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGetGraph",
			Description: "Return the knowledge graph as nodes and edges (JSON).",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"bundle": bundleParam},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGetGraph, get_graph, graph, nodes, edges, json",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			return svc.GetGraph(), nil
		},
	}
}

func getGraphSummaryTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfGetGraphSummary",
			Description: "Get a summary of the knowledge graph: total nodes, edges, and breakdown by concept type.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"bundle": bundleParam},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfGetGraphSummary, get_graph_summary, graph, summary, statistics",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			svc, err := resolveSvc(r, args)
			if err != nil {
				return nil, err
			}
			return svc.GetGraphSummary(), nil
		},
	}
}

func presentInvestigationTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "okfPresentInvestigation",
			Description: "Present a structured investigation report with summary, hypotheses, evidence, and blind spots.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"report": map[string]any{
						"type":        "object",
						"description": "Structured investigation report object",
					},
				},
				"required": []string{"report"},
			},
			Group: tool.ToolGroupExtended,
			SearchHint: "okfPresentInvestigation, present_investigation, investigation, report, summary",
		},
		Handler: func(_ *tool.ToolContext, args map[string]any) (any, error) {
			report, ok := args["report"]
			if !ok {
				return nil, fmt.Errorf("report is required")
			}
			return map[string]any{"status": "ok", "report": report}, nil
		},
	}
}
