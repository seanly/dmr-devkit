package credentials

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteSecretTempFile writes data to a new temp file with 0600 permissions.
// Returns the absolute path. Caller is responsible for cleanup.
func WriteSecretTempFile(data []byte) (path string, err error) {
	dir := os.TempDir()
	f, err := os.CreateTemp(dir, "dmr-cred-*.tmp")
	if err != nil {
		return "", fmt.Errorf("credentials: temp file: %w", err)
	}
	path = f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("credentials: chmod: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("credentials: write: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}
