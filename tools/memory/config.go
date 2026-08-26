package memory

import (
	"path/filepath"
	"strings"
)

// Config holds memory storage and attachment settings.
type Config struct {
	SQLiteDSN   string
	PostgresDSN string
	EnableFTS5  bool
	HasFTS5     bool // set after init
	PromptLimit int
	ExportDir   string // directory for markdown export/backup

	// Attachment blob storage
	AttachmentBackend      string // local | s3
	AttachmentRoot         string // local directory (absolute after parse)
	AttachmentBucket       string
	AttachmentRegion       string
	AttachmentEndpoint     string
	AttachmentAccessKey    string
	AttachmentSecretKey    string
	AttachmentKeyPrefix    string
	AttachmentUsePathStyle bool
	AttachmentMaxBytes     int64
	AttachInlineMaxBytes   int64
}

// ParseConfig maps a plugin-style config map onto Config.
// Relative paths are resolved against baseDir. There is no ~/.dmr default:
// the caller must pass baseDir (DMR plugin uses config_base_dir or ~/.dmr)
// or set SQLiteDSN / PostgresDSN on Config directly.
func ParseConfig(config map[string]any, baseDir string) Config {
	mc := Config{
		EnableFTS5:           true,
		PromptLimit:          2000,
		AttachmentBackend:    "local",
		AttachmentKeyPrefix:  "memory-attachments",
		AttachmentMaxBytes:   100 << 20,
		AttachInlineMaxBytes: 65536,
	}

	if strings.TrimSpace(baseDir) == "" {
		if v, ok := config["config_base_dir"].(string); ok && v != "" {
			baseDir = v
		}
	}

	// Top-level config
	if v, ok := config["postgres_dsn"].(string); ok && v != "" {
		mc.PostgresDSN = v
	}
	if v, ok := config["sqlite_dsn"].(string); ok && v != "" {
		if filepath.IsAbs(v) {
			mc.SQLiteDSN = v
		} else if baseDir != "" {
			mc.SQLiteDSN = filepath.Join(baseDir, v)
		} else {
			mc.SQLiteDSN = v
		}
	}
	if v, ok := config["enable_fts5"].(bool); ok {
		mc.EnableFTS5 = v
	}
	if v, ok := config["enable_fts5"].(int); ok {
		mc.EnableFTS5 = v != 0
	}
	if v, ok := config["enable_fts5"].(float64); ok {
		mc.EnableFTS5 = v != 0
	}
	if v, ok := config["prompt_limit"].(int); ok && v > 0 {
		mc.PromptLimit = v
	}
	if v, ok := config["prompt_limit"].(float64); ok && v > 0 {
		mc.PromptLimit = int(v)
	}

	if v, ok := config["export_dir"].(string); ok && v != "" {
		if filepath.IsAbs(v) {
			mc.ExportDir = v
		} else if baseDir != "" {
			mc.ExportDir = filepath.Join(baseDir, v)
		} else {
			mc.ExportDir = v
		}
	}

	if mc.SQLiteDSN == "" && baseDir != "" {
		mc.SQLiteDSN = filepath.Join(baseDir, "memory", "memory.db")
	}

	if v, ok := config["attachment_backend"].(string); ok && strings.TrimSpace(v) != "" {
		mc.AttachmentBackend = strings.ToLower(strings.TrimSpace(v))
	}
	if v, ok := config["attachment_root"].(string); ok && v != "" {
		if filepath.IsAbs(v) {
			mc.AttachmentRoot = filepath.Clean(v)
		} else if baseDir != "" {
			mc.AttachmentRoot = filepath.Clean(filepath.Join(baseDir, v))
		} else {
			mc.AttachmentRoot = filepath.Clean(v)
		}
	}
	if mc.AttachmentRoot == "" && baseDir != "" {
		mc.AttachmentRoot = filepath.Join(baseDir, "memory", "attachments")
	}
	if v, ok := config["attachment_bucket"].(string); ok {
		mc.AttachmentBucket = v
	}
	if v, ok := config["attachment_region"].(string); ok {
		mc.AttachmentRegion = v
	}
	if v, ok := config["attachment_endpoint"].(string); ok {
		mc.AttachmentEndpoint = strings.TrimSpace(v)
	}
	if v, ok := config["attachment_access_key"].(string); ok {
		mc.AttachmentAccessKey = v
	}
	if v, ok := config["attachment_secret_key"].(string); ok {
		mc.AttachmentSecretKey = v
	}
	if v, ok := config["attachment_key_prefix"].(string); ok && strings.TrimSpace(v) != "" {
		mc.AttachmentKeyPrefix = strings.Trim(strings.TrimSpace(v), "/")
	}
	if v, ok := config["attachment_use_path_style"].(bool); ok {
		mc.AttachmentUsePathStyle = v
	}
	if v, ok := config["attachment_use_path_style"].(int); ok {
		mc.AttachmentUsePathStyle = v != 0
	}
	if v, ok := config["attachment_use_path_style"].(float64); ok {
		mc.AttachmentUsePathStyle = v != 0
	}
	if v, ok := config["attachment_max_bytes"].(int64); ok && v > 0 {
		mc.AttachmentMaxBytes = v
	}
	if v, ok := config["attachment_max_bytes"].(int); ok && v > 0 {
		mc.AttachmentMaxBytes = int64(v)
	}
	if v, ok := config["attachment_max_bytes"].(float64); ok && v > 0 {
		mc.AttachmentMaxBytes = int64(v)
	}
	if v, ok := config["attach_inline_max_bytes"].(int64); ok && v > 0 {
		mc.AttachInlineMaxBytes = v
	}
	if v, ok := config["attach_inline_max_bytes"].(int); ok && v > 0 {
		mc.AttachInlineMaxBytes = int64(v)
	}
	if v, ok := config["attach_inline_max_bytes"].(float64); ok && v > 0 {
		mc.AttachInlineMaxBytes = int64(v)
	}
	if mc.AttachmentRegion == "" {
		mc.AttachmentRegion = "us-east-1"
	}

	return mc
}
