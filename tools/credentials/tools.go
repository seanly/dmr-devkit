// Package credentials provides named-credential management tools (credentialsPut/List/Search/Describe/Update/Delete).
package credentials

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cred "github.com/seanly/dmr-devkit/credentials"
	"github.com/seanly/dmr-devkit/tool"
)

type toolset struct {
	store cred.Store
}

// Tools returns the six credentials tools bound to store.
func Tools(store cred.Store) []*tool.Tool {
	p := &toolset{store: store}
	return []*tool.Tool{
		p.putTool(),
		p.listTool(),
		p.searchTool(),
		p.describeTool(),
		p.updateTool(),
		p.deleteTool(),
	}
}

func (p *toolset) putTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "credentialsPut",
			Description: "Create or replace a named credential. Use source_path (relative to workspace) for files, or inline/structured fields per kind.",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "credential secret create store save",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":              map[string]any{"type": "string", "description": "Credential id [a-zA-Z0-9._-]+"},
					"kind":            map[string]any{"type": "string", "description": "secretFile | secretText | usernamePassword | sshPrivateKey"},
					"description":     map[string]any{"type": "string", "description": "Optional description"},
					"source_path":     map[string]any{"type": "string", "description": "Path under workspace for secretFile or ssh private key PEM file"},
					"inline":          map[string]any{"type": "string", "description": "For secretText: plaintext. For secretFile: optional base64 of file bytes if no source_path"},
					"username":        map[string]any{"type": "string"},
					"password":        map[string]any{"type": "string"},
					"private_key_pem": map[string]any{"type": "string", "description": "PEM text for sshPrivateKey if no source_path"},
					"passphrase":      map[string]any{"type": "string"},
				},
				"required": []string{"id", "kind"},
			},
		},
		Handler:     p.putHandler,
		NeedContext: true,
	}
}

func (p *toolset) listTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "credentialsList",
			Description: "List credential ids and metadata (no secrets).",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "credential secret list all",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler:     p.listHandler,
		NeedContext: false,
	}
}

func (p *toolset) searchTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "credentialsSearch",
			Description: "Search credential ids and metadata by keyword. Preferred over credentialsList when there are many credentials.",
			Group:       tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Keywords to match against id, description and kind"},
				},
				"required": []string{"query"},
			},
		},
		Handler:     p.searchHandler,
		NeedContext: false,
	}
}

func (p *toolset) describeTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "credentialsDescribe",
			Description: "Show metadata for one credential id (no secret payload).",
			Group:       tool.ToolGroupCore,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
		},
		Handler:     p.describeHandler,
		NeedContext: false,
	}
}

func (p *toolset) updateTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "credentialsUpdate",
			Description: "Update the description of an existing credential. Does NOT modify the secret payload.",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "credential secret update description rename",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":          map[string]any{"type": "string"},
					"description": map[string]any{"type": "string", "description": "New description"},
				},
				"required": []string{"id"},
			},
		},
		Handler:     p.updateHandler,
		NeedContext: false,
	}
}

func (p *toolset) deleteTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "credentialsDelete",
			Description: "Delete a credential by id.",
			Group:       tool.ToolGroupExtended,
			SearchHint:  "credential secret delete remove",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
		},
		Handler:     p.deleteHandler,
		NeedContext: false,
	}
}

func (p *toolset) putHandler(ctx *tool.ToolContext, args map[string]any) (any, error) {
	if p.store == nil {
		return nil, fmt.Errorf("credentials store not initialized")
	}
	id, _ := argString(args, "id")
	kind, _ := argString(args, "kind")
	if err := cred.ValidateID(id); err != nil {
		return nil, err
	}
	if err := cred.ValidateKind(kind); err != nil {
		return nil, err
	}
	desc, _ := argString(args, "description")

	var data []byte
	var err error
	switch kind {
	case cred.KindSecretFile:
		data, err = p.readSecretFilePayload(ctx, args)
	case cred.KindSecretText:
		inline, ok := argString(args, "inline")
		if !ok || inline == "" {
			return nil, fmt.Errorf("secretText requires inline")
		}
		data = []byte(inline)
	case cred.KindUsernamePassword:
		u, _ := argString(args, "username")
		pw, _ := argString(args, "password")
		if u == "" || pw == "" {
			return nil, fmt.Errorf("usernamePassword requires username and password")
		}
		data, err = cred.MarshalUsernamePassword(u, pw)
	case cred.KindSSHPrivateKey:
		pemBytes, err2 := p.readSSHKeyPEM(ctx, args)
		if err2 != nil {
			return nil, err2
		}
		user, _ := argString(args, "username")
		pass, _ := argString(args, "passphrase")
		data, err = cred.MarshalSSHPrivateKey(user, string(pemBytes), pass)
	default:
		return nil, fmt.Errorf("unsupported kind %s", kind)
	}
	if err != nil {
		return nil, err
	}

	c := cred.Credential{
		ID:          id,
		Kind:        kind,
		Data:        data,
		Description: desc,
	}
	if err := p.store.Put(context.Background(), c); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id, "kind": kind}, nil
}

func (p *toolset) readSecretFilePayload(ctx *tool.ToolContext, args map[string]any) ([]byte, error) {
	sp, ok := argString(args, "source_path")
	if ok && sp != "" {
		path, err := resolveWorkspacePath(ctx, sp)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}
	inline, ok := argString(args, "inline")
	if !ok || inline == "" {
		return nil, fmt.Errorf("secretFile requires source_path or inline (base64)")
	}
	return base64.StdEncoding.DecodeString(inline)
}

func (p *toolset) readSSHKeyPEM(ctx *tool.ToolContext, args map[string]any) ([]byte, error) {
	sp, ok := argString(args, "source_path")
	if ok && sp != "" {
		path, err := resolveWorkspacePath(ctx, sp)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}
	pemStr, ok := argString(args, "private_key_pem")
	if ok && pemStr != "" {
		return []byte(pemStr), nil
	}
	inline, ok := argString(args, "inline")
	if ok && inline != "" {
		return []byte(inline), nil
	}
	return nil, fmt.Errorf("sshPrivateKey requires source_path, private_key_pem, or inline PEM")
}

func resolveWorkspacePath(ctx *tool.ToolContext, raw string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("credentials plugin requires a workspace context")
	}
	ws := ctx.GetCwd()
	if ws == "" {
		return "", fmt.Errorf("credentials plugin requires a workspace to be configured")
	}

	wsClean := filepath.Clean(ws)
	// Resolve workspace symlinks to get the real physical path for security checks.
	wsReal, err := filepath.EvalSymlinks(wsClean)
	if err != nil {
		wsReal = wsClean
	}

	var resolved string
	if filepath.IsAbs(raw) {
		resolved = filepath.Clean(raw)
	} else {
		resolved = filepath.Clean(filepath.Join(wsClean, raw))
	}

	// Resolve symlinks in the target path to prevent escape via links.
	targetReal, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		// File doesn't exist (e.g. for write): walk up to find the nearest
		// existing ancestor and resolve symlinks from there.
		targetReal = evalSymlinksWalkUp(resolved)
	}

	// Security check: ensure the real physical path is within the real workspace.
	sep := string(filepath.Separator)
	if targetReal != wsReal && !strings.HasPrefix(targetReal, wsReal+sep) {
		return "", fmt.Errorf("path %q escapes workspace %q", targetReal, wsReal)
	}
	// Return the original resolved path (not the physical path) for caller consistency.
	return resolved, nil
}

// evalSymlinksWalkUp resolves symlinks by walking up the directory tree until
// it finds an existing ancestor. This handles the case where intermediate
// directories don't exist yet.
func evalSymlinksWalkUp(path string) string {
	var tail []string
	cur := path
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			result := real
			for i := len(tail) - 1; i >= 0; i-- {
				result = filepath.Join(result, tail[i])
			}
			return result
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func (p *toolset) listHandler(_ *tool.ToolContext, _ map[string]any) (any, error) {
	if p.store == nil {
		return nil, fmt.Errorf("credentials store not initialized")
	}
	items, err := p.store.List(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, metaToMap(m))
	}
	return out, nil
}

func (p *toolset) searchHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	if p.store == nil {
		return nil, fmt.Errorf("credentials store not initialized")
	}
	query, _ := argString(args, "query")
	items, err := p.store.Search(context.Background(), query)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, metaToMap(m))
	}
	return out, nil
}

func (p *toolset) updateHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	if p.store == nil {
		return nil, fmt.Errorf("credentials store not initialized")
	}
	id, _ := argString(args, "id")
	if err := cred.ValidateID(id); err != nil {
		return nil, err
	}
	desc, _ := argString(args, "description")

	c, err := p.store.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	c.Description = desc
	if err := p.store.Put(context.Background(), c); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id, "updated": true}, nil
}

func (p *toolset) describeHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	if p.store == nil {
		return nil, fmt.Errorf("credentials store not initialized")
	}
	id, _ := argString(args, "id")
	c, err := p.store.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	m := metaToMap(cred.CredentialMeta{
		ID:          c.ID,
		Kind:        c.Kind,
		Description: c.Description,
		UpdatedAt:   c.UpdatedAt,
		Meta:        c.Meta,
	})
	if fields, ok := cred.KindFields[c.Kind]; ok {
		m["available_fields"] = fields
	}
	return m, nil
}

func (p *toolset) deleteHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	if p.store == nil {
		return nil, fmt.Errorf("credentials store not initialized")
	}
	id, _ := argString(args, "id")
	if err := cred.ValidateID(id); err != nil {
		return nil, err
	}
	if err := p.store.Delete(context.Background(), id); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": id}, nil
}

func metaToMap(m cred.CredentialMeta) map[string]any {
	meta := m.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	return map[string]any{
		"id":          m.ID,
		"kind":        m.Kind,
		"description": m.Description,
		"updated_at":  m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"meta":        meta,
	}
}

func argString(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	default:
		return fmt.Sprint(t), true
	}
}
