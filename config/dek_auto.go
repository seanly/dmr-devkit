package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	keyfileVersionV2 = 2
	verifyLen        = 8
)

// Keyfile stores the data encryption key (DEK) directly (v1 format).
// Duplicated from DMR CLI crypto (`github.com/seanly/dmr/pkg/config`) for credentials auto-load without plugin coupling.
type Keyfile struct {
	Version   int    `json:"version"`
	Key       string `json:"key"`        // base64-encoded DEK
	KeyVerify string `json:"key_verify"` // base64, SHA-256(DEK)[:8] for integrity check
	PinHash   string `json:"pin_hash"`   // SHA-256 hex of user PIN, for show --decrypt verification
}

// KeyfilePath returns ~/.dmr/.keyfile
func KeyfilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dmr", ".keyfile")
}

func keyfileVersionFromJSON(data []byte) (int, error) {
	var v struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return 0, fmt.Errorf("parse keyfile version: %w", err)
	}
	return v.Version, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var x byte
	for i := range a {
		x |= a[i] ^ b[i]
	}
	return x == 0
}

// LoadDEKAuto loads the DEK directly from a v1 keyfile (no PIN needed).
// Returns an error for v2 keyfiles — use agent-side LoadDEK(pin) instead.
func LoadDEKAuto() ([]byte, error) {
	path := KeyfilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read keyfile: %w", err)
	}

	version, err := keyfileVersionFromJSON(data)
	if err != nil {
		return nil, err
	}

	if version >= keyfileVersionV2 {
		return nil, fmt.Errorf("keyfile v%d requires PIN; use LoadDEK(pin) or provide PIN interactively", version)
	}

	var kf Keyfile
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("parse keyfile: %w", err)
	}

	dek, err := base64.StdEncoding.DecodeString(kf.Key)
	if err != nil {
		return nil, fmt.Errorf("decode DEK: %w", err)
	}

	verify, err := base64.StdEncoding.DecodeString(kf.KeyVerify)
	if err != nil {
		return nil, fmt.Errorf("decode verify: %w", err)
	}
	h := sha256.Sum256(dek)
	if !equalBytes(h[:verifyLen], verify) {
		return nil, fmt.Errorf("keyfile corrupted: verification failed")
	}

	return dek, nil
}
