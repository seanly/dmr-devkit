package tools

import (
	"fmt"
	"time"

	"github.com/seanly/dmr-devkit/tool"
	"github.com/seanly/dmr-devkit/okf/frontmatter"
	"github.com/seanly/dmr-devkit/okf/service"
)

// ProducerToolsWith returns the 5 producer (mutation) tools over a Resolver:
// create_concept, update_concept, set_frontmatter, append_section,
// delete_concept. Callers gate them behind approval / read-only mode.
func ProducerToolsWith(r Resolver) []*tool.Tool {
	return []*tool.Tool{
		createConceptTool(r),
		updateConceptTool(r),
		setFrontmatterTool(r),
		appendSectionTool(r),
		deleteConceptTool(r),
	}
}

// ProducerTools is the single-bundle back-compat wrapper.
func ProducerTools(svc *service.Service) []*tool.Tool { return ProducerToolsWith(SingleResolver(svc)) }

// --- mutation tools ---

func createConceptTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "create_concept",
			Description: "Create a new OKF concept (writes a .md file to the bundle). type is required. Use this to add knowledge fetched from external sources.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":          map[string]any{"type": "string", "description": "Bundle-relative concept id without .md (e.g. \"references/api\", \"tables/orders\")"},
					"type":        map[string]any{"type": "string", "description": "Concept type (e.g. concept, table, reference, guideline)"},
					"title":       map[string]any{"type": "string", "description": "Human-readable title"},
					"description": map[string]any{"type": "string", "description": "Short summary"},
					"body":        map[string]any{"type": "string", "description": "Markdown body content"},
					"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"sources":     map[string]any{"type": "array", "description": "Provenance sources (each needs at least a resource URL)", "items": map[string]any{"type": "object"}},
					"bundle":      bundleParam,
				},
				"required": []string{"id", "type", "body"},
			},
			Group: tool.ToolGroupCore,
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
			meta, err := buildMeta(args)
			if err != nil {
				return nil, err
			}
			body, _ := stringParam(args, "body")
			return svc.CreateConcept(id, meta, body)
		},
	}
}

func updateConceptTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "update_concept",
			Description: "Replace a concept's markdown body, preserving its frontmatter.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "Concept id"},
					"body":   map[string]any{"type": "string", "description": "New markdown body"},
					"bundle": bundleParam,
				},
				"required": []string{"id", "body"},
			},
			Group: tool.ToolGroupCore,
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
			body, _ := stringParam(args, "body")
			return svc.UpdateConcept(id, body)
		},
	}
}

func setFrontmatterTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "set_frontmatter",
			Description: "Set a single frontmatter field on an existing concept. Allowed fields: type, title, description, resource, status, stale_after, tags.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "Concept id"},
					"field":  map[string]any{"type": "string", "description": "Frontmatter field name"},
					"value":  map[string]any{"description": "Field value (string; tags expects array of strings)"},
					"bundle": bundleParam,
				},
				"required": []string{"id", "field", "value"},
			},
			Group: tool.ToolGroupCore,
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
			field, err := requiredString(args, "field")
			if err != nil {
				return nil, err
			}
			value, ok := args["value"]
			if !ok {
				return nil, fmt.Errorf("value is required")
			}
			return svc.SetFrontmatter(id, field, value)
		},
	}
}

func appendSectionTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "append_section",
			Description: "Append a section (## heading + content) to a concept's body.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":      map[string]any{"type": "string", "description": "Concept id"},
					"heading": map[string]any{"type": "string", "description": "Section heading (without the ## prefix)"},
					"content": map[string]any{"type": "string", "description": "Section markdown content"},
					"bundle":  bundleParam,
				},
				"required": []string{"id", "heading", "content"},
			},
			Group: tool.ToolGroupCore,
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
			heading, _ := stringParam(args, "heading")
			content, err := requiredString(args, "content")
			if err != nil {
				return nil, err
			}
			return svc.AppendToConcept(id, heading, content)
		},
	}
}

func deleteConceptTool(r Resolver) *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "delete_concept",
			Description: "Delete a concept (removes its .md file from the bundle). Irreversible.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "Concept id"},
					"bundle": bundleParam,
				},
				"required": []string{"id"},
			},
			Group: tool.ToolGroupCore,
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
			return svc.DeleteConcept(id)
		},
	}
}

// --- helpers ---

// buildMeta constructs a *frontmatter.Meta from create_concept tool args,
// auto-setting provenance (generated) for traceability of machine-authored
// concepts per OKF §5.2.
func buildMeta(args map[string]any) (*frontmatter.Meta, error) {
	typ, err := requiredString(args, "type")
	if err != nil {
		return nil, err
	}
	meta := &frontmatter.Meta{
		Type:        typ,
		Title:       getString(args, "title"),
		Description: getString(args, "description"),
		Resource:    getString(args, "resource"),
		Tags:        getStringSlice(args, "tags"),
		Generated: &frontmatter.ActorEvent{
			By: "agent:okf",
			At: time.Now().UTC(),
		},
	}
	if raw, ok := args["sources"]; ok && raw != nil {
		sources, err := parseSources(raw)
		if err != nil {
			return nil, err
		}
		meta.Sources = sources
	}
	return meta, nil
}

func parseSources(raw any) ([]frontmatter.Source, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("sources must be an array")
	}
	out := make([]frontmatter.Source, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("sources[%d] must be an object", i)
		}
		res, _ := m["resource"].(string)
		if res == "" {
			return nil, fmt.Errorf("sources[%d].resource is required", i)
		}
		s := frontmatter.Source{
			ID:       getString(m, "id"),
			Resource: res,
			Title:    getString(m, "title"),
			Author:   getString(m, "author"),
		}
		out = append(out, s)
	}
	return out, nil
}

func getString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func getStringSlice(m map[string]any, key string) []string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		// tolerate []string
		if ss, ok := raw.([]string); ok {
			return ss
		}
		return nil
	}
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
