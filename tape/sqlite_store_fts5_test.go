package tape

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/config"
)

// TestSQLiteFTS5DisabledByDefault tests FTS5 is disabled by default
func TestSQLiteFTS5DisabledByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if store.IsFTSEnabled() {
		t.Error("FTS5 should be disabled by default")
	}
}

// TestSQLiteFTS5EnableTrue tests FTS5 enabled with true
func TestSQLiteFTS5EnableTrue(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if !store.IsFTSEnabled() {
		t.Error("FTS5 should be enabled when config is true")
	}

	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='entries_fts'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check fts5 table: %v", err)
	}
	if count != 1 {
		t.Error("entries_fts table should exist")
	}
}

// TestSQLiteFTS5AutoMode tests auto mode
func TestSQLiteFTS5AutoMode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5Auto,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if !store.IsFTSEnabled() {
		t.Error("FTS5 should be enabled in auto mode for new database")
	}
}

// TestSQLiteFTS5Migration tests data migration
func TestSQLiteFTS5Migration(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Step 1: Create store with FTS5 disabled, add data
	store1, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5False,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	testEntries := []TapeEntry{
		{Kind: "message", Payload: map[string]any{"content": "我爱编程"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "hello world"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "bug fix required"}, Date: time.Now().Format(time.RFC3339)},
	}

	for _, entry := range testEntries {
		_ = store1.Append("testtape", entry)
	}
	store1.Close()

	// Step 2: Enable FTS5, should trigger migration
	store2, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store with FTS5: %v", err)
	}
	defer store2.Close()

	var count int
	err = store2.db.QueryRow("SELECT COUNT(*) FROM entries_fts").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count fts entries: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 fts entries, got %d", count)
	}
}

// TestSQLiteFTS5Search tests FTS5 search functionality
func TestSQLiteFTS5Search(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	testEntries := []TapeEntry{
		{Kind: "message", Payload: map[string]any{"content": "我爱编程 Go语言"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "编程是乐趣"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "hello world bug"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "fix the bug"}, Date: time.Now().Format(time.RFC3339)},
	}

	for _, entry := range testEntries {
		_ = store.Append("testtape", entry)
	}

	t.Run("ChineseSearch", func(t *testing.T) {
		opts := &FetchOpts{
			TextQuery: "爱编程",
			Limit:     10,
		}
		entries, err := store.FetchAllSearch("testtape", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result for '爱编程', got %d", len(entries))
		}
	})

	t.Run("EnglishSearch", func(t *testing.T) {
		opts := &FetchOpts{
			TextQuery: "bug",
			Limit:     10,
		}
		entries, err := store.FetchAllSearch("testtape", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("expected 2 results for 'bug', got %d", len(entries))
		}
	})
}

// TestSQLiteFTS5SpecialChars tests queries containing FTS5-sensitive characters
// like dots (IP addresses) and hyphens (compound words) that previously caused
// syntax errors when passed unquoted to the MATCH expression.
func TestSQLiteFTS5SpecialChars(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	testEntries := []TapeEntry{
		{Kind: "message", Payload: map[string]any{"content": "curl http://10.200.22.253:8000/v1/model"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "reasoning-parser thinking enable_thinking"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "normal content here"}, Date: time.Now().Format(time.RFC3339)},
	}
	for _, entry := range testEntries {
		_ = store.Append("testtape", entry)
	}

	t.Run("IPAddressWithDots", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "10.200.22.253", Limit: 10}
		entries, err := store.FetchAllSearch("testtape", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result for IP query, got %d", len(entries))
		}
	})

	t.Run("HyphenatedWord", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "reasoning-parser", Limit: 10}
		entries, err := store.FetchAllSearch("testtape", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result for hyphenated query, got %d", len(entries))
		}
	})

	t.Run("URLWithSpecialChars", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "http://10.200.22.253:8000/v1/model", Limit: 10}
		entries, err := store.FetchAllSearch("testtape", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result for URL query, got %d", len(entries))
		}
	})
}

// TestSQLiteFTS5Trigger tests trigger auto-sync
func TestSQLiteFTS5Trigger(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	_ = store.Append("testtape", TapeEntry{
		Kind:    "message",
		Payload: map[string]any{"content": "trigger test content"},
		Date:    time.Now().Format(time.RFC3339),
	})

	opts := &FetchOpts{
		TextQuery: "trigger",
	}
	entries, err := store.FetchAllSearch("testtape", opts, "fts5")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 result, got %d", len(entries))
	}
}

// TestSQLiteFTS5MultiTape tests FTS5 searches only within specified tape
func TestSQLiteFTS5MultiTape(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	tapes := []string{"tape1", "tape2", "feishu:p2p:oc_test123"}
	for _, tapeName := range tapes {
		_ = store.Append(tapeName, TapeEntry{
			Kind:    "message",
			Payload: map[string]any{"content": "shared keyword content"},
			Date:    time.Now().Format(time.RFC3339),
		})
		_ = store.Append(tapeName, TapeEntry{
			Kind:    "message",
			Payload: map[string]any{"content": "unique content for " + tapeName},
			Date:    time.Now().Format(time.RFC3339),
		})
	}

	t.Run("SearchInSpecificTape", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "shared"}
		entries, err := store.FetchAllSearch("tape1", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result in tape1, got %d", len(entries))
		}
	})

	t.Run("SearchInSpecialTapeName", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "shared"}
		entries, err := store.FetchAllSearch("feishu:p2p:oc_test123", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result in feishu tape, got %d", len(entries))
		}
		content, _ := entries[0].Payload["content"].(string)
		if content != "shared keyword content" {
			t.Errorf("unexpected content: %s", content)
		}
	})

	t.Run("SearchUniqueContent", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "tape2"}
		entries, err := store.FetchAllSearch("tape2", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result for 'tape2' in tape2, got %d", len(entries))
		}

		entries, err = store.FetchAllSearch("tape1", opts, "fts5")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 results for 'tape2' in tape1, got %d", len(entries))
		}
	})
}

// TestSQLiteFTS5ForceSearchMode tests force search mode via FetchAllSearch
func TestSQLiteFTS5ForceSearchMode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	_ = store.Append("testtape", TapeEntry{
		Kind:    "message",
		Payload: map[string]any{"content": "force mode test"},
		Date:    time.Now().Format(time.RFC3339),
	})

	t.Run("ForceFTS5", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "force"}
		entries, err := store.FetchAllSearch("testtape", opts, "fts5")
		if err != nil {
			t.Fatalf("force fts5 search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result, got %d", len(entries))
		}
	})

	t.Run("ForceLIKE", func(t *testing.T) {
		opts := &FetchOpts{TextQuery: "mode"}
		entries, err := store.FetchAllSearch("testtape", opts, "like")
		if err != nil {
			t.Fatalf("force like search failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 result, got %d", len(entries))
		}
	})
}

// TestSQLiteFTS5RuntimeToggle tests runtime enable/disable
func TestSQLiteFTS5RuntimeToggle(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if !store.IsFTSEnabled() {
		t.Fatal("FTS5 should be enabled initially")
	}

	err = store.SetFTSEnabled(false)
	if err != nil {
		t.Fatalf("failed to disable FTS5: %v", err)
	}
	if store.IsFTSEnabled() {
		t.Error("FTS5 should be disabled after SetFTSEnabled(false)")
	}

	err = store.SetFTSEnabled(true)
	if err != nil {
		t.Fatalf("failed to enable FTS5: %v", err)
	}
	if !store.IsFTSEnabled() {
		t.Error("FTS5 should be enabled after SetFTSEnabled(true)")
	}
}

// TestSQLiteFTS5Rebuild tests rebuild index
func TestSQLiteFTS5Rebuild(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 5; i++ {
		_ = store.Append("testtape", TapeEntry{
			Kind:    "message",
			Payload: map[string]any{"content": "rebuild test"},
			Date:    time.Now().Format(time.RFC3339),
		})
	}

	err = store.RebuildFTS()
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}

	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM entries_fts").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count after rebuild: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 entries after rebuild, got %d", count)
	}
}

// TestSQLiteFTS5Migration_ContextCancel verifies that completeMigration respects
// context cancellation and exits early instead of running indefinitely.
func TestSQLiteFTS5Migration_ContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Step 1: Create store with FTS5 enabled, add data, then clear FTS5 index
	store, err := NewSQLiteTapeStore(dbPath, SQLiteStoreConfig{
		EnableFTS5: config.FTS5True,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	for i := 0; i < 5; i++ {
		_ = store.Append("testtape", TapeEntry{
			Kind:    "message",
			Payload: map[string]any{"content": "migration test data"},
			Date:    time.Now().Format(time.RFC3339),
		})
	}

	// Clear FTS5 index to simulate unmigrated state
	_, _ = store.db.Exec("DELETE FROM entries_fts")

	// Test completeMigration directly with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = store.completeMigration(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled from completeMigration, got: %v", err)
	}

	// Test migrateWithTimeout with an already-cancelled context
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	err = store.migrateWithTimeout(ctx2)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") && err != context.Canceled {
		t.Errorf("expected timeout or context canceled error, got: %v", err)
	}

	store.Close()
}
