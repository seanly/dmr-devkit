package memory

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/seanly/dmr-devkit/tool"
)

func attachToolStr(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func attachBoolArg(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

func (p *Service) memoryAttachPutTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name: "memoryAttachPut",
			Description: "Upload a binary attachment bound to an existing memory page (slug). " +
				"Provide exactly one of base64 or workspacePath. Binary is stored outside SQL; name and summary are indexed for search.",
			Group:      tool.ToolGroupExtended,
			SearchHint: "memory attachment file blob upload binary",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string", "description": "Existing page slug this attachment belongs to"},
					"name": map[string]any{"type": "string", "description": "Logical filename for listing and FTS"},
					"base64": map[string]any{
						"type":        "string",
						"description": "Base64 body; use either this or workspacePath",
					},
					"workspacePath": map[string]any{
						"type":        "string",
						"description": "Path relative to workspace for an existing file",
					},
					"mimeType": map[string]any{"type": "string", "description": "MIME type (default application/octet-stream)"},
					"summary": map[string]any{
						"type":        "string",
						"description": "Optional searchable description (indexed with name)",
					},
				},
				"required": []string{"slug", "name"},
			},
		},
		Handler: p.handleMemoryAttachPut,
	}
}

func (p *Service) handleMemoryAttachPut(ctx *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}
	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}
	name := strings.TrimSpace(attachToolStr(args, "name"))
	if name == "" {
		return map[string]any{"success": false, "error": "name is required"}, nil
	}
	b64 := strings.TrimSpace(attachToolStr(args, "base64"))
	wsPath := strings.TrimSpace(attachToolStr(args, "workspacePath"))
	if (b64 == "" && wsPath == "") || (b64 != "" && wsPath != "") {
		return map[string]any{"success": false, "error": "provide exactly one of base64 or workspacePath"}, nil
	}
	mime := strings.TrimSpace(attachToolStr(args, "mimeType"))
	summary := attachToolStr(args, "summary")

	var body []byte
	var err error
	if b64 != "" {
		body, err = base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return map[string]any{"success": false, "error": "invalid base64: " + err.Error()}, nil
		}
	} else {
		if ctx == nil {
			return map[string]any{"success": false, "error": "workspacePath requires tool context"}, nil
		}
		abs, err := resolveWorkspacePath(ctx, wsPath)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		body, err = os.ReadFile(abs)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
	}
	sz := int64(len(body))
	if sz > p.config.AttachmentMaxBytes {
		return map[string]any{"success": false, "error": fmt.Sprintf("content size %d exceeds attachment_max_bytes %d", sz, p.config.AttachmentMaxBytes)}, nil
	}

	tctx := context.Background()
	if ctx != nil && ctx.Ctx != nil {
		tctx = ctx.Ctx
	}
	a, err := p.backend.PutAttachment(tctx, slug, AttachmentInput{Name: name, Mime: mime, Summary: summary}, bytes.NewReader(body), sz)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{
		"success":            true,
		"id":                 a.PublicID,
		"slug":               a.Slug,
		"name":               a.Name,
		"size":               a.Size,
		"mime":               a.Mime,
		"storage_kind":       a.StorageKind,
		"attachment_backend": p.config.AttachmentBackend,
	}, nil
}

func (p *Service) memoryAttachGetTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name: "memoryAttachGet",
			Description: "Read attachment bytes by id. For large files set intoWorkspaceRelativePath; " +
				"for small files asBase64 true returns data under attach_inline_max_bytes.",
			Group:      tool.ToolGroupExtended,
			SearchHint: "memory attachment download fetch blob",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Attachment id from memoryAttachPut"},
					"intoWorkspaceRelativePath": map[string]any{
						"type":        "string",
						"description": "Write bytes to this path relative to workspace",
					},
					"asBase64": map[string]any{
						"type":        "boolean",
						"description": "If true, include base64 when under inline size limit",
					},
				},
				"required": []string{"id"},
			},
		},
		Handler: p.handleMemoryAttachGet,
	}
}

func (p *Service) handleMemoryAttachGet(ctx *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}
	id := strings.TrimSpace(attachToolStr(args, "id"))
	if _, err := uuid.Parse(id); err != nil {
		return map[string]any{"success": false, "error": "invalid attachment id"}, nil
	}
	outPath := strings.TrimSpace(attachToolStr(args, "intoWorkspaceRelativePath"))
	asB64 := attachBoolArg(args, "asBase64")

	tctx := context.Background()
	if ctx != nil && ctx.Ctx != nil {
		tctx = ctx.Ctx
	}
	meta, err := p.backend.GetAttachment(id)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if meta == nil {
		return map[string]any{"success": false, "error": "attachment not found"}, nil
	}

	if outPath != "" {
		if ctx == nil {
			return map[string]any{"success": false, "error": "intoWorkspaceRelativePath requires tool context"}, nil
		}
		rc, _, err := p.backend.OpenAttachment(tctx, id)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return map[string]any{"success": false, "error": readErr.Error()}, nil
		}
		abs, err := writeWorkspaceFile(ctx, outPath, data)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		return map[string]any{
			"success":         true,
			"id":              id,
			"slug":            meta.Slug,
			"name":            meta.Name,
			"workspacePath":   outPath,
			"size":            len(data),
			"mime":            meta.Mime,
			"writtenAbsolute": abs,
		}, nil
	}

	if meta.Size > p.config.AttachInlineMaxBytes {
		return map[string]any{
			"success": false,
			"error": fmt.Sprintf("attachment size %d exceeds attach_inline_max_bytes %d; set intoWorkspaceRelativePath",
				meta.Size, p.config.AttachInlineMaxBytes),
		}, nil
	}

	rc, _, err := p.backend.OpenAttachment(tctx, id)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if int64(len(data)) > p.config.AttachInlineMaxBytes {
		return map[string]any{"success": false, "error": "content exceeded inline limit after read"}, nil
	}

	if !asB64 {
		return map[string]any{
			"success": true,
			"id":      id,
			"slug":    meta.Slug,
			"name":    meta.Name,
			"size":    len(data),
			"mime":    meta.Mime,
			"summary": meta.Summary,
			"hint":    "set asBase64 true to include base64 data",
		}, nil
	}
	return map[string]any{
		"success":    true,
		"id":         id,
		"slug":       meta.Slug,
		"name":       meta.Name,
		"size":       len(data),
		"mime":       meta.Mime,
		"summary":    meta.Summary,
		"dataBase64": base64.StdEncoding.EncodeToString(data),
	}, nil
}

func (p *Service) memoryAttachListTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryAttachList",
			Description: "List attachments for a page by slug (newest first).",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "memory attachment catalog list",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug":  map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer", "description": "Max entries", "default": 50},
				},
				"required": []string{"slug"},
			},
		},
		Handler: p.handleMemoryAttachList,
	}
}

func (p *Service) handleMemoryAttachList(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}
	slug, bad := requireSlugArg(args["slug"], "slug")
	if bad != nil {
		return bad, nil
	}
	limit := 50
	if l, ok := intFromArgPositive(args["limit"], 500); ok {
		limit = l
	}
	list, err := p.backend.ListAttachments(slug, limit)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{"success": true, "slug": slug, "attachments": list}, nil
}

func (p *Service) memoryAttachDeleteTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryAttachDelete",
			Description: "Delete an attachment by id (metadata and blob).",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "memory attachment remove",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
		},
		Handler: p.handleMemoryAttachDelete,
	}
}

func (p *Service) handleMemoryAttachDelete(ctx *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}
	id := strings.TrimSpace(attachToolStr(args, "id"))
	if _, err := uuid.Parse(id); err != nil {
		return map[string]any{"success": false, "error": "invalid attachment id"}, nil
	}
	tctx := context.Background()
	if ctx != nil && ctx.Ctx != nil {
		tctx = ctx.Ctx
	}
	if err := p.backend.DeleteAttachment(tctx, id); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{"success": true, "id": id}, nil
}

func (p *Service) memoryAttachInfoTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "memoryAttachInfo",
			Description: "Return metadata for an attachment id (no bytes).",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "memory attachment metadata stat",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
		},
		Handler: p.handleMemoryAttachInfo,
	}
}

func (p *Service) handleMemoryAttachInfo(_ *tool.ToolContext, args map[string]any) (any, error) {
	if resp, stop := p.requireBackend(); stop {
		return resp, nil
	}
	id := strings.TrimSpace(attachToolStr(args, "id"))
	if _, err := uuid.Parse(id); err != nil {
		return map[string]any{"success": false, "error": "invalid attachment id"}, nil
	}
	meta, err := p.backend.GetAttachment(id)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if meta == nil {
		return map[string]any{"success": false, "error": "attachment not found"}, nil
	}
	return map[string]any{
		"success":      true,
		"id":           meta.PublicID,
		"slug":         meta.Slug,
		"name":         meta.Name,
		"mime":         meta.Mime,
		"size":         meta.Size,
		"summary":      meta.Summary,
		"storage_kind": meta.StorageKind,
		"created_at":   meta.CreatedAt,
		"updated_at":   meta.UpdatedAt,
	}, nil
}
