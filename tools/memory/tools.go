package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/seanly/dmr-devkit/tool"
)

const slugHelp = `
Slug naming: lowercase alphanumeric + hyphens, / separated. Prefix by type:
  config/* (preferences), people/* (persons), companies/*, meetings/*, projects/*, concepts/*, notes/*
Before writing, memorySearch first to reuse existing slug.`

// lookupFlowHelp documents keyword-only search (no embeddings) and a three-step lookup flow.
const lookupFlowHelp = "Lookup: (1) If you know the exact slug, use memoryGet (fastest, full page). " +
	"(2) For names, keywords, or to avoid duplicate pages, use memorySearch (full-text only, not semantic/embedding). " +
	"(3) memoryList filters by tag/type/slug prefix. " +
	"After memorySearch, call memoryGet for the full page when a snippet is not enough (high-stakes or nuance). "

// --- memoryPut ---

func (p *Service) memoryPutTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryPut",
			Description: "Create or update a knowledge page. " + slugHelp + " " + lookupFlowHelp,
			Group:       tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug":        map[string]any{"type": "string", "description": "Page identifier, e.g. people/tina-wang"},
					"title":       map[string]any{"type": "string", "description": "Page title"},
					"type":        map[string]any{"type": "string", "description": "Page type: note, person, company, meeting, concept, config, episode, semantic, procedural", "default": "note"},
					"content":     map[string]any{"type": "string", "description": "Page content"},
					"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to set on the page"},
					"frontmatter": map[string]any{"type": "object", "description": "Optional metadata as JSON object"},
				},
				"required": []string{"slug", "content"},
			},
		},
		Handler: p.handleMemoryPut,
	}
}

func (p *Service) handleMemoryPut(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	slug, _ := args["slug"].(string)
	slug = strings.ToLower(strings.TrimSpace(slug))
	if err := ValidateSlug(slug); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}

	content, _ := args["content"].(string)
	if strings.TrimSpace(content) == "" {
		return map[string]any{"success": false, "error": "content must not be empty"}, nil
	}
	if err := scanContent(content); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}

	title, _ := args["title"].(string)
	typ, _ := args["type"].(string)
	typ = strings.TrimSpace(typ)
	if err := ValidatePageType(typ); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if typ != "" {
		typ = strings.ToLower(typ)
	}

	var fmStr string
	if fm, ok := args["frontmatter"]; ok && fm != nil {
		if _, ok := fm.(map[string]any); !ok {
			return map[string]any{"success": false, "error": "frontmatter must be a JSON object"}, nil
		}
		b, err := json.Marshal(fm)
		if err != nil {
			return map[string]any{"success": false, "error": "invalid frontmatter: " + err.Error()}, nil
		}
		fmStr = string(b)
	}

	var tags []string
	if t, ok := args["tags"].([]any); ok {
		for _, v := range t {
			s, ok := v.(string)
			if !ok {
				return map[string]any{"success": false, "error": "tags must be an array of strings"}, nil
			}
			tags = append(tags, s)
		}
	}

	page, err := p.backend.PutPage(slug, PageInput{
		Type:        typ,
		Title:       title,
		Content:     content,
		Frontmatter: fmStr,
		Tags:        tags,
	})
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}

	return map[string]any{
		"success": true,
		"slug":    page.Slug,
		"type":    page.Type,
		"updated": true,
	}, nil
}

// --- memoryGet ---

func (p *Service) memoryGetTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name: "memoryGet",
			Description: "Read a full page by slug (title, content, frontmatter, tags, timeline, links). " +
				"Prefer this when the slug is known, or right after memorySearch to load complete text. " + lookupFlowHelp,
			Group: tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string", "description": "Page identifier"},
				},
				"required": []string{"slug"},
			},
		},
		Handler: p.handleMemoryGet,
	}
}

func (p *Service) handleMemoryGet(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}

	page, err := p.backend.GetPage(slug)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if page == nil {
		return map[string]any{"success": false, "error": fmt.Sprintf("page %q not found", slug)}, nil
	}

	links, _ := p.backend.GetLinks(slug)
	return pageToDetail(page, links), nil
}

func pageToDetail(p *Page, links []LinkInfo) map[string]any {
	m := map[string]any{
		"slug":        p.Slug,
		"type":        p.Type,
		"title":       p.Title,
		"content":     p.Content,
		"frontmatter": json.RawMessage(p.Frontmatter),
		"created_at":  p.CreatedAt,
		"updated_at":  p.UpdatedAt,
	}
	if len(p.Tags) > 0 {
		m["tags"] = p.Tags
	}
	if len(p.Timeline) > 0 {
		m["timeline"] = p.Timeline
	}
	if len(links) > 0 {
		m["links"] = links
	}
	return m
}

// --- memoryDelete ---

func (p *Service) memoryDeleteTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryDelete",
			Description: "Delete a knowledge page and all associated tags, links, and timeline entries.",
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string", "description": "Page identifier"},
				},
				"required": []string{"slug"},
			},
		},
		Handler: p.handleMemoryDelete,
	}
}

func (p *Service) handleMemoryDelete(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}

	if err := p.backend.DeletePage(slug); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{"success": true, "slug": slug}, nil
}

// --- memorySearch ---

func (p *Service) memorySearchTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memorySearch",
			Description: "Full-text search (keyword, not embedding). Returns short snippets. " + lookupFlowHelp,
			Group:       tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query"},
					"limit": map[string]any{"type": "integer", "description": "Max results", "default": 5},
					"include_attachments": map[string]any{
						"type":        "boolean",
						"description": "Also search attachment names/summaries and append hits (default false)",
						"default":     false,
					},
				},
				"required": []string{"query"},
			},
		},
		Handler: p.handleMemorySearch,
	}
}

func (p *Service) handleMemorySearch(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	query, _ := args["query"].(string)
	if utf8.RuneCountInString(query) > MaxSearchQueryRunes {
		return map[string]any{"success": false, "error": fmt.Sprintf("query exceeds %d characters", MaxSearchQueryRunes)}, nil
	}

	limit := 5
	if l, ok := intFromArgPositive(args["limit"], 100); ok {
		limit = l
	}
	includeAttachments := attachBoolArg(args, "include_attachments")

	results, err := p.backend.SearchPages(query, limit)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	var att []AttachmentSearchResult
	if includeAttachments {
		alimit := limit
		if alimit > 20 {
			alimit = 20
		}
		var aerr error
		att, aerr = p.backend.SearchAttachments(query, alimit)
		if aerr != nil {
			return map[string]any{"success": false, "error": aerr.Error()}, nil
		}
	}

	if len(results) == 0 && len(att) == 0 {
		return "No results found.", nil
	}

	var lines []string
	for i, r := range results {
		lines = append(lines, fmt.Sprintf("%d. [%s] %s (%s)\n   %s", i+1, r.Slug, r.Title, r.Type, r.Snippet))
	}
	if includeAttachments && len(att) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "Attachments:")
		for i, r := range att {
			lines = append(lines, fmt.Sprintf("%d. id=%s [%s] %s\n   %s", i+1, r.PublicID, r.Slug, r.Name, r.Snippet))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// --- memoryList ---

func (p *Service) memoryListTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryList",
			Description: "List knowledge pages (metadata lines) with optional filters by tag, type, slug prefix, or frontmatter key-value pairs. " + lookupFlowHelp,
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tag":                 map[string]any{"type": "string", "description": "Filter by tag"},
					"type":                map[string]any{"type": "string", "description": "Filter by type"},
					"slug_prefix":         map[string]any{"type": "string", "description": "Filter by slug prefix, e.g. 'people/'"},
					"limit":               map[string]any{"type": "integer", "description": "Max results", "default": 20},
					"offset":              map[string]any{"type": "integer", "description": "Offset for pagination"},
					"frontmatter_filter":  map[string]any{"type": "object", "description": "Filter by frontmatter key-value pairs (exact string match). Example: {\"status\": \"unprocessed\"}"},
					"include_frontmatter": map[string]any{"type": "boolean", "description": "If true, return structured JSON array with full page metadata including parsed frontmatter. Default false returns text list.", "default": false},
				},
			},
		},
		Handler: p.handleMemoryList,
	}
}

func (p *Service) handleMemoryList(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	opts := ListOpts{Limit: 20}

	if v, ok := args["tag"].(string); ok {
		tag := strings.ToLower(strings.TrimSpace(v))
		if err := ValidateTagFilter(tag); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		opts.Tag = tag
	}
	if v, ok := args["type"].(string); ok {
		typ := strings.TrimSpace(v)
		if err := ValidatePageType(typ); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		if typ != "" {
			opts.Type = strings.ToLower(typ)
		}
	}
	if v, ok := args["slug_prefix"].(string); ok {
		prefix := strings.ToLower(strings.TrimSpace(v))
		if err := ValidateSlugPrefix(prefix); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		opts.SlugPrefix = prefix
	}
	if lim, ok := intFromArgPositive(args["limit"], 500); ok {
		opts.Limit = lim
	}
	if off, ok := intFromArgNonNeg(args["offset"], 1_000_000); ok {
		opts.Offset = off
	}

	// Parse frontmatter_filter
	if fmRaw, ok := args["frontmatter_filter"]; ok && fmRaw != nil {
		if fmMap, ok := fmRaw.(map[string]any); ok {
			opts.FrontmatterFilter = make(map[string]string, len(fmMap))
			for k, v := range fmMap {
				if s, ok := v.(string); ok {
					opts.FrontmatterFilter[k] = s
				} else {
					opts.FrontmatterFilter[k] = fmt.Sprintf("%v", v)
				}
			}
		}
	}

	includeFm := false
	if v, ok := args["include_frontmatter"]; ok {
		includeFm = boolFromAny(v, false)
	}

	pages, err := p.backend.ListPages(opts)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if len(pages) == 0 {
		return "No pages found.", nil
	}

	if includeFm {
		var result []map[string]any
		for _, pg := range pages {
			m := map[string]any{
				"slug":       pg.Slug,
				"type":       pg.Type,
				"title":      pg.Title,
				"updated_at": pg.UpdatedAt.Format(time.RFC3339),
			}
			if pg.Frontmatter != "" && pg.Frontmatter != "{}" {
				m["frontmatter"] = json.RawMessage(pg.Frontmatter)
			}
			result = append(result, m)
		}
		return map[string]any{"success": true, "pages": result, "count": len(result)}, nil
	}

	var lines []string
	for _, pg := range pages {
		lines = append(lines, fmt.Sprintf("- [%s] %s (%s) updated %s", pg.Slug, pg.Title, pg.Type, pg.UpdatedAt.Format("2006-01-02")))
	}
	return strings.Join(lines, "\n"), nil
}

// --- memoryLink ---

func (p *Service) memoryLinkTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryLink",
			Description: "Create a directional link (from -> to) with an explicit link_type. " + LinkTypesForTools,
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"from_slug": map[string]any{"type": "string", "description": "Source page slug"},
					"to_slug":   map[string]any{"type": "string", "description": "Target page slug"},
					"link_type": map[string]any{"type": "string", "description": "Relationship type, e.g. mentions, works_at, invested_in"},
					"context":   map[string]any{"type": "string", "description": "Optional context for the link"},
				},
				"required": []string{"from_slug", "to_slug", "link_type"},
			},
		},
		Handler: p.handleMemoryLink,
	}
}

func (p *Service) handleMemoryLink(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	from, bad := requireSlugArg(args["from_slug"], "from_slug")
	if bad != nil {
		return bad, nil
	}
	to, bad := requireSlugArg(args["to_slug"], "to_slug")
	if bad != nil {
		return bad, nil
	}
	linkTypeStr, _ := args["link_type"].(string)
	linkType := strings.TrimSpace(linkTypeStr)
	context, _ := args["context"].(string)
	if err := scanContent(context); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}

	if err := p.backend.CreateLink(from, to, linkType, context); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{"success": true, "from": from, "to": to, "link_type": linkType}, nil
}

// --- memoryUnlink ---

func (p *Service) memoryUnlinkTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryUnlink",
			Description: "Remove a link between two pages; link_type must match the edge you added. " + LinkTypesForTools,
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"from_slug": map[string]any{"type": "string", "description": "Source page slug"},
					"to_slug":   map[string]any{"type": "string", "description": "Target page slug"},
					"link_type": map[string]any{"type": "string", "description": "Link type to remove"},
				},
				"required": []string{"from_slug", "to_slug", "link_type"},
			},
		},
		Handler: p.handleMemoryUnlink,
	}
}

func (p *Service) handleMemoryUnlink(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	from, bad := requireSlugArg(args["from_slug"], "from_slug")
	if bad != nil {
		return bad, nil
	}
	to, bad := requireSlugArg(args["to_slug"], "to_slug")
	if bad != nil {
		return bad, nil
	}
	linkTypeStr, _ := args["link_type"].(string)
	linkType := strings.TrimSpace(linkTypeStr)

	if err := p.backend.DeleteLink(from, to, linkType); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{"success": true}, nil
}

// --- memoryLinks ---

func (p *Service) memoryLinksTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name: "memoryLinks",
			Description: "List links touching this page. Each item has direction: outgoing = this page points to other_slug; " +
				"incoming = other page points to this one. " + LinkTypesForTools,
			Group: tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string", "description": "Page identifier"},
				},
				"required": []string{"slug"},
			},
		},
		Handler: p.handleMemoryLinks,
	}
}

func (p *Service) handleMemoryLinks(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}

	links, err := p.backend.GetLinks(slug)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if len(links) == 0 {
		return "No links found.", nil
	}

	return links, nil
}

// --- memoryTags ---

func (p *Service) memoryTagsTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryTags",
			Description: "Manage tags on a page: get, add, or remove tags.",
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug":   map[string]any{"type": "string", "description": "Page identifier"},
					"action": map[string]any{"type": "string", "enum": []string{"get", "add", "remove"}, "description": "Action to perform"},
					"tags":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to add/remove (required for add/remove)"},
				},
				"required": []string{"slug", "action"},
			},
		},
		Handler: p.handleMemoryTags,
	}
}

func (p *Service) handleMemoryTags(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}
	action, _ := args["action"].(string)

	switch action {
	case "get":
		tags, err := p.backend.GetTags(slug)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		if len(tags) == 0 {
			return "No tags.", nil
		}
		return tags, nil

	case "add":
		tags, perr := parseTagsArg(args["tags"])
		if perr != nil {
			return perr, nil
		}
		if len(tags) == 0 {
			return map[string]any{"success": false, "error": "tags required for add action"}, nil
		}
		if err := p.backend.AddTags(slug, tags); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true}, nil

	case "remove":
		tags, perr := parseTagsArg(args["tags"])
		if perr != nil {
			return perr, nil
		}
		if len(tags) == 0 {
			return map[string]any{"success": false, "error": "tags required for remove action"}, nil
		}
		if err := p.backend.RemoveTags(slug, tags); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true}, nil

	default:
		return map[string]any{"success": false, "error": "unknown action: " + action}, nil
	}
}

// --- memoryTimeline ---

func (p *Service) memoryTimelineTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryTimeline",
			Description: "Read or add timeline entries for a page.",
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug":    map[string]any{"type": "string", "description": "Page identifier"},
					"action":  map[string]any{"type": "string", "enum": []string{"get", "add"}, "description": "Action: get timeline or add entry"},
					"date":    map[string]any{"type": "string", "description": "Event date (YYYY-MM-DD), required for add"},
					"summary": map[string]any{"type": "string", "description": "Event summary, required for add"},
				},
				"required": []string{"slug", "action"},
			},
		},
		Handler: p.handleMemoryTimeline,
	}
}

func (p *Service) handleMemoryTimeline(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}

	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}
	action, _ := args["action"].(string)

	switch action {
	case "get":
		entries, err := p.backend.GetTimeline(slug)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		if len(entries) == 0 {
			return "No timeline entries.", nil
		}
		return entries, nil

	case "add":
		date, _ := args["date"].(string)
		summary, _ := args["summary"].(string)
		if date == "" || summary == "" {
			return map[string]any{"success": false, "error": "date and summary required for add action"}, nil
		}
		if err := ValidateTimelineDate(date); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		if err := scanContent(summary); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		if err := p.backend.AddTimelineEntry(slug, TimelineEntry{EventDate: date, Summary: summary}); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true, "date": date}, nil

	default:
		return map[string]any{"success": false, "error": "unknown action: " + action}, nil
	}
}

// --- memoryStatus ---

func (p *Service) memoryStatusTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryStatus",
			Description: "Stats for the memory store: backend (sqlite or postgres), full-text index on/off, page and revision counts, and a data-source hint (e.g. sqlite file name). Use to confirm the store is healthy.",
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler: p.handleMemoryStatus,
	}
}

func (p *Service) handleMemoryStatus(_ *tool.ToolContext, _ map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}
	s, err := p.backend.Stats()
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{
		"success":          true,
		"backend":          s.Backend,
		"fulltext":         s.Fulltext,
		"page_count":       s.PageCount,
		"revision_count":   s.RevisionCount,
		"data_source_hint": s.DataSource,
	}, nil
}

// --- memoryRevisions ---

func (p *Service) memoryRevisionsTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryRevisions",
			Description: "List prior snapshots of a page (newest first) or revert the page to a snapshot. Revert saves the current version as a new snapshot first, then restores title/type/content/frontmatter. Tags, links, and timeline are not reverted.",
			Group:       tool.ToolGroupExtended,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "Page slug",
					},
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"list", "revert"},
						"description": "list: return revision metadata; revert: restore to revision_id",
					},
					"revision_id": map[string]any{
						"type":        "integer",
						"description": "Required for revert: id from a list entry (memory_page_revisions.id)",
					},
				},
				"required": []string{"slug", "action"},
			},
		},
		Handler: p.handleMemoryRevisions,
	}
}

func (p *Service) handleMemoryRevisions(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}
	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}
	action, _ := args["action"].(string)
	switch action {
	case "list":
		revs, err := p.backend.ListRevisions(slug)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		if len(revs) == 0 {
			return "No revisions (only created when page body/title/type/frontmatter change).", nil
		}
		return map[string]any{"success": true, "revisions": revs}, nil
	case "revert":
		rid, ok := intFromArg(args["revision_id"])
		if !ok || rid <= 0 {
			return map[string]any{"success": false, "error": "revision_id required and must be a positive integer"}, nil
		}
		if err := p.backend.RevertToRevision(slug, rid); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{"success": true, "slug": slug, "reverted_to_revision_id": rid}, nil
	default:
		return map[string]any{"success": false, "error": "unknown action: " + action}, nil
	}
}

// --- helpers ---

func toolErr(msg string) map[string]any {
	return map[string]any{"success": false, "error": msg}
}

// requireSlugArg lowercases/trims and validates slug shape; second return is a tool result map on failure.
func requireSlugArg(raw any, field string) (string, map[string]any) {
	if raw == nil {
		return "", toolErr(field + ": missing")
	}
	s, ok := raw.(string)
	if !ok {
		return "", toolErr(field + ": must be a string")
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if err := ValidateSlug(s); err != nil {
		return "", toolErr(field + ": " + err.Error())
	}
	return s, nil
}

func intFromArg(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// intFromArgPositive returns a capped positive int when v is present and valid.
func intFromArgPositive(v any, maxCap int) (int, bool) {
	if v == nil {
		return 0, false
	}
	n, ok := intFromArg(v)
	if !ok || n <= 0 {
		return 0, false
	}
	if n > maxCap {
		n = maxCap
	}
	return n, true
}

func intFromArgNonNeg(v any, maxCap int) (int, bool) {
	if v == nil {
		return 0, false
	}
	n, ok := intFromArg(v)
	if !ok || n < 0 {
		return 0, false
	}
	if n > maxCap {
		n = maxCap
	}
	return n, true
}

func parseTagsArg(v any) ([]string, map[string]any) {
	if v == nil {
		return nil, toolErr("tags must be an array of strings")
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, toolErr("tags must be an array of strings")
	}
	var out []string
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, toolErr("tags must be an array of strings")
		}
		out = append(out, strings.TrimSpace(strings.ToLower(s)))
	}
	return out, nil
}

// boolFromAny coerces a JSON/tool argument to bool with a default fallback.
func boolFromAny(v any, defaultVal bool) bool {
	if v == nil {
		return defaultVal
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		if s == "" {
			return defaultVal
		}
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	default:
		return defaultVal
	}
}
