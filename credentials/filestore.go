package credentials

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seanly/dmr-devkit/config"
)

// FileStore stores one JSON file per credential under an absolute root directory.
type FileStore struct {
	root       string
	dek        []byte // 32 bytes when encryption enabled
	allowPlain bool
}

// NewFileStore creates a file-backed store. root must be an absolute path.
// If dek is non-nil (len 32), credentials are encrypted at rest.
// If dek is nil, allowPlain must be true to permit writes (insecure).
func NewFileStore(root string, dek []byte, allowPlain bool) (*FileStore, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("credentials: file store root must be absolute: %s", root)
	}
	if dek != nil && len(dek) != 32 {
		return nil, fmt.Errorf("credentials: DEK must be 32 bytes")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("credentials: mkdir %s: %w", root, err)
	}
	return &FileStore{root: root, dek: dek, allowPlain: allowPlain}, nil
}

func init() {
	RegisterDriver("file", func(opts OpenOpts) (Store, error) {
		base := opts.ConfigBase
		dir := strings.TrimSpace(opts.Dir)
		if dir == "" {
			dir = "credentials"
		}
		var storeRoot string
		if filepath.IsAbs(dir) {
			storeRoot = filepath.Clean(dir)
		} else {
			storeRoot = filepath.Join(base, filepath.Clean(dir))
		}

		allowPlain := opts.AllowInsecurePlaintext
		var dek []byte
		if opts.DEK != nil {
			dek = opts.DEK
		} else {
			dek, _ = config.LoadDEKAuto()
		}
		return NewFileStore(storeRoot, dek, allowPlain)
	})
}

func (f *FileStore) bindPath(id string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(f.root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(absRoot, id+".json")
	absP, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absP)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("credentials: invalid id path")
	}
	return absP, nil
}

func (f *FileStore) encodeDoc(c Credential) (*diskDoc, error) {
	NormalizeCredential(&c)
	doc := &diskDoc{
		Version:     fileVersion,
		ID:          c.ID,
		Kind:        c.Kind,
		UpdatedAt:   c.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Description: c.Description,
		Meta:        c.Meta,
	}
	if f.dek != nil {
		nb, pb, err := EncryptPayload(f.dek, c.Data)
		if err != nil {
			return nil, err
		}
		doc.Encrypted = true
		doc.NonceB64 = nb
		doc.PayloadB64 = pb
		return doc, nil
	}
	if !f.allowPlain {
		return nil, fmt.Errorf("credentials: cannot write without DEK")
	}
	doc.Encrypted = false
	doc.PayloadB64 = base64.StdEncoding.EncodeToString(c.Data)
	return doc, nil
}

func (f *FileStore) decodeDoc(doc *diskDoc) (Credential, error) {
	var data []byte
	var err error
	if doc.Encrypted {
		if f.dek == nil {
			return Credential{}, fmt.Errorf("credentials: encrypted entry but no DEK")
		}
		data, err = DecryptPayload(f.dek, doc.NonceB64, doc.PayloadB64)
		if err != nil {
			return Credential{}, err
		}
	} else {
		if !f.allowPlain && f.dek == nil {
			return Credential{}, fmt.Errorf("credentials: plaintext entry not allowed")
		}
		data, err = base64.StdEncoding.DecodeString(doc.PayloadB64)
		if err != nil {
			return Credential{}, fmt.Errorf("credentials: decode payload: %w", err)
		}
	}
	t, _ := parseRFC3339(doc.UpdatedAt)
	meta := doc.Meta
	if meta == nil {
		meta = make(map[string]string)
	}
	return Credential{
		ID:          doc.ID,
		Kind:        doc.Kind,
		Data:        data,
		Description: doc.Description,
		Meta:        meta,
		UpdatedAt:   t,
	}, nil
}

// GetMeta returns metadata for a credential without decrypting the payload.
func (f *FileStore) GetMeta(ctx context.Context, id string) (CredentialMeta, error) {
	_ = ctx
	p, err := f.bindPath(id)
	if err != nil {
		return CredentialMeta{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return CredentialMeta{}, fmt.Errorf("credentials: not found: %s", id)
		}
		return CredentialMeta{}, err
	}
	var doc diskDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return CredentialMeta{}, fmt.Errorf("credentials: parse %s: %w", id, err)
	}
	return metaFromDoc(&doc), nil
}

// Get implements Store.
func (f *FileStore) Get(ctx context.Context, id string) (Credential, error) {
	_ = ctx
	p, err := f.bindPath(id)
	if err != nil {
		return Credential{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Credential{}, fmt.Errorf("credentials: not found: %s", id)
		}
		return Credential{}, err
	}
	var doc diskDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Credential{}, fmt.Errorf("credentials: parse %s: %w", id, err)
	}
	return f.decodeDoc(&doc)
}

// Put implements Store.
func (f *FileStore) Put(ctx context.Context, c Credential) error {
	_ = ctx
	if err := ValidateID(c.ID); err != nil {
		return err
	}
	if err := ValidateKind(c.Kind); err != nil {
		return err
	}
	doc, err := f.encodeDoc(c)
	if err != nil {
		return err
	}
	p, err := f.bindPath(c.ID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Delete implements Store.
func (f *FileStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	p, err := f.bindPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Search implements Store.
func (f *FileStore) Search(ctx context.Context, query string) ([]CredentialMeta, error) {
	items, err := f.List(ctx)
	if err != nil {
		return nil, err
	}
	return SearchCredentials(items, query), nil
}

// List implements Store.
func (f *FileStore) List(ctx context.Context) ([]CredentialMeta, error) {
	_ = ctx
	entries, err := os.ReadDir(f.root)
	if err != nil {
		return nil, err
	}
	var out []CredentialMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if ValidateID(id) != nil {
			continue
		}
		p := filepath.Join(f.root, e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			slog.Warn("credential filestore: skip unreadable file", "path", p, "error", err)
			continue
		}
		var doc diskDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			slog.Warn("credential filestore: skip corrupted file", "path", p, "error", err)
			continue
		}
		out = append(out, metaFromDoc(&doc))
	}
	return out, nil
}
