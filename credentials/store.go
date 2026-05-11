// Package credentials provides a named credential store for DMR (file/mem backends, AES-GCM with config DEK).
package credentials

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Kind values (Jenkins-style).
const (
	KindUsernamePassword = "usernamePassword"
	KindSSHPrivateKey    = "sshPrivateKey"
	KindSecretFile       = "secretFile"
	KindSecretText       = "secretText"
)

// MaxIDLen is the maximum credential id length.
const MaxIDLen = 128

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Credential is the in-memory representation (plaintext Data).
type Credential struct {
	ID          string
	Kind        string
	Data        []byte
	Description string
	Meta        map[string]string
	UpdatedAt   time.Time
}

// CredentialMeta is returned by List (no secret payload).
type CredentialMeta struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Description string            `json:"description,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// Store persists credentials by id.
type Store interface {
	Get(ctx context.Context, id string) (Credential, error)
	Put(ctx context.Context, c Credential) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]CredentialMeta, error)
	Search(ctx context.Context, query string) ([]CredentialMeta, error)
}

// drivers is the global registry of credential store backends.
var drivers = make(map[string]func(OpenOpts) (Store, error))

// RegisterDriver registers a credential store backend by name.
// Built-in drivers (file, mem) register themselves in init().
func RegisterDriver(name string, factory func(OpenOpts) (Store, error)) {
	drivers[name] = factory
}

var (
	defaultMu sync.RWMutex
	defaultS  Store
)

// Register sets the process-wide default store (typically from credentials plugin Init).
func Register(s Store) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultS = s
}

// Default returns the registered store or ErrNoStore.
func Default() (Store, error) {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	if defaultS == nil {
		return nil, ErrNoStore
	}
	return defaultS, nil
}

// ErrNoStore is returned when no store has been registered.
var ErrNoStore = errors.New("credentials: no store registered")

// ValidateID returns an error if id is not a safe credential identifier.
func ValidateID(id string) error {
	if id == "" {
		return fmt.Errorf("credentials: id is required")
	}
	if len(id) > MaxIDLen {
		return fmt.Errorf("credentials: id too long (max %d)", MaxIDLen)
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("credentials: id must match [a-zA-Z0-9._-]+")
	}
	return nil
}

// KindFields returns the available field names for each credential kind.
// Used by credentialsDescribe to inform LLM what fields can be bound.
var KindFields = map[string][]string{
	KindSecretText:       {"value"},
	KindSecretFile:       {"file"},
	KindUsernamePassword: {"username", "password"},
	KindSSHPrivateKey:    {"username", "privateKey", "passphrase"},
}

// ValidateKind checks kind is one of the supported constants.
func ValidateKind(kind string) error {
	switch kind {
	case KindUsernamePassword, KindSSHPrivateKey, KindSecretFile, KindSecretText:
		return nil
	default:
		return fmt.Errorf("credentials: unknown kind %q", kind)
	}
}

// SearchCredentials filters and scores credential metadata by query.
// Empty query returns all items. Matches are sorted by relevance (most matched words first).
func SearchCredentials(items []CredentialMeta, query string) []CredentialMeta {
	if query == "" {
		return items
	}
	words := splitSearchWords(query)
	if len(words) == 0 {
		return items
	}
	type scored struct {
		item  CredentialMeta
		score int
	}
	var matches []scored
	for _, item := range items {
		searchable := strings.ToLower(item.ID + " " + item.Description + " " + item.Kind)
		score := 0
		for _, word := range words {
			if strings.Contains(searchable, word) {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, scored{item, score})
		}
	}
	// Sort by score descending.
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].score > matches[i].score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
	out := make([]CredentialMeta, len(matches))
	for i, m := range matches {
		out[i] = m.item
	}
	return out
}

func splitSearchWords(s string) []string {
	var words []string
	for _, w := range strings.Fields(s) {
		if w != "" {
			words = append(words, strings.ToLower(w))
		}
	}
	return words
}

// NormalizeCredential sets UpdatedAt if zero.
func NormalizeCredential(c *Credential) {
	if c.Meta == nil {
		c.Meta = make(map[string]string)
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = time.Now().UTC()
	}
}
