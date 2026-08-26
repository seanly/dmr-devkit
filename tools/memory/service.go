// Package memory provides a structured knowledge base with pages, links, tags,
// timeline, and attachments. Call Open then Service.Tools() to obtain agent tools.
package memory

import (
	"context"
	"fmt"

	"github.com/seanly/dmr-devkit/tool"
)

// Service is a memory knowledge-base instance that exposes agent tools.
type Service struct {
	backend Backend
	blob    AttachmentBlobStore
	config  Config
}

// Open constructs a Service from cfg. If blob is nil, a local filesystem blob
// store is created from cfg.AttachmentRoot.
func Open(ctx context.Context, cfg Config, blob AttachmentBlobStore) (*Service, error) {
	_ = ctx
	var err error
	if blob == nil {
		blob, err = NewLocalBlobStore(cfg)
		if err != nil {
			return nil, fmt.Errorf("memory attachments blob store: %w", err)
		}
	}

	var backend Backend
	if cfg.PostgresDSN != "" {
		backend, err = NewPostgresBackend(cfg, blob)
		if err != nil {
			_ = blob.Close()
			return nil, fmt.Errorf("create memory postgres backend: %w", err)
		}
	} else {
		if cfg.SQLiteDSN == "" {
			_ = blob.Close()
			return nil, fmt.Errorf("memory: sqlite_dsn or postgres_dsn is required")
		}
		backend, err = NewSQLiteBackend(cfg, blob)
		if err != nil {
			_ = blob.Close()
			return nil, fmt.Errorf("create memory sqlite backend: %w", err)
		}
	}
	cfg.HasFTS5 = backend.HasFTS5()
	return &Service{backend: backend, blob: blob, config: cfg}, nil
}

// Tools returns the memory tool set.
func (s *Service) Tools() []*tool.Tool {
	if s == nil {
		return nil
	}
	return []*tool.Tool{
		s.memoryPutTool(),
		s.memoryGetTool(),
		s.memoryDeleteTool(),
		s.memorySearchTool(),
		s.memoryListTool(),
		s.memoryLinkTool(),
		s.memoryUnlinkTool(),
		s.memoryLinksTool(),
		s.memoryTagsTool(),
		s.memoryTimelineTool(),
		s.memoryStatusTool(),
		s.memoryRevisionsTool(),
		s.memoryAttachPutTool(),
		s.memoryAttachGetTool(),
		s.memoryAttachListTool(),
		s.memoryAttachDeleteTool(),
		s.memoryAttachInfoTool(),
	}
}

// Close shuts down the backend and blob store.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.backend != nil {
		err = s.backend.Close()
	}
	if s.blob != nil {
		if e := s.blob.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// Config returns a copy of the service configuration.
func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.config
}
