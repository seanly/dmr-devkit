package memory

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// AttachmentLocalStore writes blobs as files under root/{publicID} (flat layout).
type AttachmentLocalStore struct {
	root     string
	maxBytes int64
}

// NewAttachmentLocalStore creates a filesystem blob store.
func NewAttachmentLocalStore(root string, maxBytes int64) *AttachmentLocalStore {
	return &AttachmentLocalStore{root: root, maxBytes: maxBytes}
}

func (b *AttachmentLocalStore) Close() error { return nil }

func (b *AttachmentLocalStore) blobPath(id string) string {
	return filepath.Join(b.root, id)
}

func (b *AttachmentLocalStore) Put(ctx context.Context, publicID, _ string, body io.Reader, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := uuid.Parse(publicID); err != nil {
		return fmt.Errorf("invalid attachment id: %w", err)
	}
	if size < 0 || size > b.maxBytes {
		return fmt.Errorf("attachment size %d out of bounds (max %d)", size, b.maxBytes)
	}
	if err := os.MkdirAll(b.root, 0o755); err != nil {
		return err
	}
	bp := b.blobPath(publicID)
	f, err := os.OpenFile(bp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, err := io.CopyN(f, body, size)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(bp)
		return err
	}
	if n != size {
		_ = os.Remove(bp)
		return fmt.Errorf("attachment upload short read: got %d want %d", n, size)
	}
	return nil
}

func (b *AttachmentLocalStore) Open(ctx context.Context, publicID string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(publicID); err != nil {
		return nil, fmt.Errorf("invalid attachment id: %w", err)
	}
	return os.Open(b.blobPath(publicID))
}

func (b *AttachmentLocalStore) Delete(ctx context.Context, publicID string) error {
	_ = ctx
	if _, err := uuid.Parse(publicID); err != nil {
		return fmt.Errorf("invalid attachment id: %w", err)
	}
	return os.Remove(b.blobPath(publicID))
}
