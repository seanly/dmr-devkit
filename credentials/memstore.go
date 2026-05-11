package credentials

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/config"
)

// MemStore is an in-memory Store (same JSON envelope as file, for tests).
type MemStore struct {
	mu         sync.RWMutex
	docs       map[string]*diskDoc // id -> doc
	dek        []byte
	allowPlain bool
}

// NewMemStore creates a memory store with the same encryption rules as FileStore.
func NewMemStore(dek []byte, allowPlain bool) (*MemStore, error) {
	if dek != nil && len(dek) != 32 {
		return nil, fmt.Errorf("credentials: DEK must be 32 bytes")
	}
	if dek == nil && !allowPlain {
		return nil, fmt.Errorf("credentials: encryption required (no DEK and allow_insecure_plaintext is false)")
	}
	return &MemStore{
		docs:       make(map[string]*diskDoc),
		dek:        dek,
		allowPlain: allowPlain,
	}, nil
}

func init() {
	RegisterDriver("mem", func(opts OpenOpts) (Store, error) {
		allowPlain := opts.AllowInsecurePlaintext
		var dek []byte
		if opts.DEK != nil {
			dek = opts.DEK
		} else {
			dek, _ = config.LoadDEKAuto()
		}
		if dek == nil && !allowPlain {
			return nil, fmt.Errorf("credentials: need DEK — run 'dmr config keygen' or set allow_insecure_plaintext")
		}
		return NewMemStore(dek, allowPlain)
	})
}

func (m *MemStore) encodeDoc(c Credential) (*diskDoc, error) {
	NormalizeCredential(&c)
	doc := &diskDoc{
		Version:     fileVersion,
		ID:          c.ID,
		Kind:        c.Kind,
		UpdatedAt:   c.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Description: c.Description,
		Meta:        c.Meta,
	}
	if m.dek != nil {
		nb, pb, err := EncryptPayload(m.dek, c.Data)
		if err != nil {
			return nil, err
		}
		doc.Encrypted = true
		doc.NonceB64 = nb
		doc.PayloadB64 = pb
		return doc, nil
	}
	if !m.allowPlain {
		return nil, fmt.Errorf("credentials: cannot write without DEK")
	}
	doc.Encrypted = false
	doc.PayloadB64 = base64.StdEncoding.EncodeToString(c.Data)
	return doc, nil
}

func (m *MemStore) decodeDoc(doc *diskDoc) (Credential, error) {
	fs := &FileStore{dek: m.dek, allowPlain: m.allowPlain}
	return fs.decodeDoc(doc)
}

// Get implements Store.
func (m *MemStore) Get(ctx context.Context, id string) (Credential, error) {
	_ = ctx
	if err := ValidateID(id); err != nil {
		return Credential{}, err
	}
	m.mu.RLock()
	doc, ok := m.docs[id]
	m.mu.RUnlock()
	if !ok {
		return Credential{}, fmt.Errorf("credentials: not found: %s", id)
	}
	return m.decodeDoc(doc)
}

// Put implements Store.
func (m *MemStore) Put(ctx context.Context, c Credential) error {
	_ = ctx
	if err := ValidateID(c.ID); err != nil {
		return err
	}
	if err := ValidateKind(c.Kind); err != nil {
		return err
	}
	doc, err := m.encodeDoc(c)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.docs[c.ID] = doc
	m.mu.Unlock()
	return nil
}

// Delete implements Store.
func (m *MemStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	if err := ValidateID(id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.docs, id)
	m.mu.Unlock()
	return nil
}

// Search implements Store.
func (m *MemStore) Search(ctx context.Context, query string) ([]CredentialMeta, error) {
	items, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	return SearchCredentials(items, query), nil
}

// List implements Store.
func (m *MemStore) List(ctx context.Context) ([]CredentialMeta, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]CredentialMeta, 0, len(m.docs))
	for _, doc := range m.docs {
		d := *doc
		out = append(out, metaFromDoc(&d))
	}
	return out, nil
}

// SnapshotJSON returns a copy of raw JSON per id (for tests).
func (m *MemStore) SnapshotJSON() (map[string][]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]byte, len(m.docs))
	for id, doc := range m.docs {
		b, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		out[id] = b
	}
	return out, nil
}
