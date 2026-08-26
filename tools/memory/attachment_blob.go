package memory

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// AttachmentBlobStore stores raw attachment bytes keyed by public_id (UUID string).
type AttachmentBlobStore interface {
	Put(ctx context.Context, publicID, mime string, body io.Reader, size int64) error
	Open(ctx context.Context, publicID string) (io.ReadCloser, error)
	Delete(ctx context.Context, publicID string) error
	Close() error
}

// NewLocalBlobStore builds a filesystem blob store from config.
// S3/MinIO stores are constructed by the DMR plugin, which injects them into Open.
func NewLocalBlobStore(cfg Config) (AttachmentBlobStore, error) {
	if cfg.AttachmentRoot == "" {
		return nil, fmt.Errorf("memory attachments: attachment_root is empty")
	}
	return NewAttachmentLocalStore(cfg.AttachmentRoot, cfg.AttachmentMaxBytes), nil
}

// NewAttachmentBlobStore builds local blob storage from config.
func NewAttachmentBlobStore(_ context.Context, cfg Config) (AttachmentBlobStore, error) {
	bk := strings.ToLower(strings.TrimSpace(cfg.AttachmentBackend))
	switch bk {
	case "s3", "minio":
		return nil, fmt.Errorf("memory attachments: s3/minio blob store is not built into this package; pass a blob into Open")
	case "local", "fs", "file", "":
		return NewLocalBlobStore(cfg)
	default:
		return nil, fmt.Errorf("memory attachments: unknown attachment_backend %q", cfg.AttachmentBackend)
	}
}
