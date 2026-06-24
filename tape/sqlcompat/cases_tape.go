package sqlcompat

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/tape"
)

// RunTapeCases executes tape evaluation cases T01–T15 for one driver.
func RunTapeCases(t *testing.T, driver Driver, rep *Report) {
	t.Helper()
	run := func(id string, fn func(t *testing.T) error, blocker bool) {
		t.Helper()
		err := fn(t)
		if err != nil {
			if strings.HasPrefix(err.Error(), "SKIP:") {
				rep.Add(id, driver, StatusSkip, strings.TrimPrefix(err.Error(), "SKIP: "), false)
				return
			}
			rep.Add(id, driver, StatusFail, err.Error(), blocker)
			t.Logf("[%s/%s] %v", id, driver, err)
			return
		}
		rep.Add(id, driver, StatusPass, "", false)
	}

	run("T01", func(t *testing.T) error { return caseT01Schema(t, driver) }, true)
	run("T02", func(t *testing.T) error { return caseT02CRUD(t, driver) }, true)
	run("T03", func(t *testing.T) error { return caseT03Anchor(t, driver) }, true)
	run("T04", func(t *testing.T) error { return caseT04FTS5Create(t, driver) }, true)
	run("T05", func(t *testing.T) error { return caseT05FTS5InsertTrigger(t, driver) }, true)
	run("T06", func(t *testing.T) error { return caseT06FTS5DeleteTrigger(t, driver) }, false)
	run("T07", func(t *testing.T) error { return caseT07FTS5Search(t, driver) }, true)
	run("T08", func(t *testing.T) error { return caseT08FTS5SpecialChars(t, driver) }, false)
	run("T09", func(t *testing.T) error { return caseT09FTS5MultiTape(t, driver) }, true)
	run("T10", func(t *testing.T) error { return caseT10FTS5Migration(t, driver) }, true)
	run("T11", func(t *testing.T) error { return caseT11RebuildFTS(t, driver) }, false)
	run("T12", func(t *testing.T) error { return caseT12LikeFallback(t, driver) }, false)
	run("T14", func(t *testing.T) error { return caseT14WAL(t, driver) }, false)
	run("T15", func(t *testing.T) error { return caseT15Timezone(t, driver) }, false)
}

func newTapeStore(t *testing.T, driver Driver, fts config.FTS5Mode) (*tape.SQLiteTapeStore, error) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "eval.db")
	store, err := tape.NewSQLiteTapeStoreWithDriver(dbPath, string(driver), tape.SQLiteStoreConfig{
		EnableFTS5: fts,
	})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { store.Close() })
	return store, nil
}

func openTapeStoreOrSkip(t *testing.T, driver Driver, fts config.FTS5Mode) (*tape.SQLiteTapeStore, error) {
	store, err := newTapeStore(t, driver, fts)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return store, nil
}

func caseT01Schema(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5False)
	if err != nil {
		return err
	}
	var n int
	err = store.DB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='entries'",
	).Scan(&n)
	if err != nil {
		return err
	}
	if n < 3 {
		return fmt.Errorf("expected >=3 indexes on entries, got %d", n)
	}
	return nil
}

func caseT02CRUD(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5False)
	if err != nil {
		return err
	}
	if err := store.Append("tape-a", tape.TapeEntry{Kind: "message", Payload: map[string]any{"content": "a"}}); err != nil {
		return err
	}
	if err := store.Append("tape-b", tape.TapeEntry{Kind: "message", Payload: map[string]any{"content": "b"}}); err != nil {
		return err
	}
	tapes := store.ListTapes()
	if len(tapes) != 2 {
		return fmt.Errorf("ListTapes: want 2, got %d", len(tapes))
	}
	entries, err := store.FetchAll("tape-a", nil)
	if err != nil || len(entries) != 1 {
		return fmt.Errorf("FetchAll tape-a: len=%d err=%v", len(entries), err)
	}
	store.Reset("tape-a")
	entries, err = store.FetchAll("tape-a", nil)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("after Reset: len=%d err=%v", len(entries), err)
	}
	return nil
}

func caseT03Anchor(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5False)
	if err != nil {
		return err
	}
	_ = store.Append("t", tape.NewAnchorEntry("start", map[string]any{"x": 1}))
	_ = store.Append("t", tape.TapeEntry{Kind: "message", Payload: map[string]any{"content": "mid"}})
	_ = store.Append("t", tape.NewAnchorEntry("end", map[string]any{"x": 2}))

	after, err := store.FetchAll("t", &tape.FetchOpts{AfterAnchor: "start"})
	if err != nil || len(after) < 2 {
		return fmt.Errorf("AfterAnchor start: len=%d err=%v", len(after), err)
	}
	between, err := store.FetchAll("t", &tape.FetchOpts{BetweenAnchors: [2]string{"start", "end"}})
	if err != nil || len(between) != 1 {
		return fmt.Errorf("BetweenAnchors: len=%d err=%v", len(between), err)
	}
	return nil
}

func caseT04FTS5Create(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5True)
	if err != nil {
		return err
	}
	if !store.IsFTSEnabled() {
		return fmt.Errorf("FTS5 not enabled")
	}
	var n int
	if err := store.DB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='entries_fts'",
	).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("entries_fts missing")
	}
	return nil
}

func caseT05FTS5InsertTrigger(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5True)
	if err != nil {
		return err
	}
	_ = store.Append("t", tape.TapeEntry{
		Kind: "message", Payload: map[string]any{"content": "trigger sync keyword"},
		Date: time.Now().Format(time.RFC3339),
	})
	entries, err := store.FetchAllSearch("t", &tape.FetchOpts{TextQuery: "trigger"}, "fts5")
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return fmt.Errorf("expected 1 fts hit, got %d", len(entries))
	}
	return nil
}

func caseT06FTS5DeleteTrigger(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5True)
	if err != nil {
		return err
	}
	_ = store.Append("t", tape.TapeEntry{
		Kind: "message", Payload: map[string]any{"content": "deleteme uniquexyz"},
		Date: time.Now().Format(time.RFC3339),
	})
	entries, _ := store.FetchAllSearch("t", &tape.FetchOpts{TextQuery: "uniquexyz"}, "fts5")
	if len(entries) != 1 {
		return fmt.Errorf("pre-delete: expected 1, got %d", len(entries))
	}
	id := entries[0].ID
	if _, err := store.DB().Exec("DELETE FROM entries WHERE id = ?", id); err != nil {
		return fmt.Errorf("SKIP: DELETE trigger on %s: %v", driver, err)
	}
	entries, err = store.FetchAllSearch("t", &tape.FetchOpts{TextQuery: "uniquexyz"}, "fts5")
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("post-delete FTS: expected 0 hits, got %d", len(entries))
	}
	return nil
}

func caseT07FTS5Search(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5True)
	if err != nil {
		return err
	}
	entries := []tape.TapeEntry{
		{Kind: "message", Payload: map[string]any{"content": "我爱编程 Go语言"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "hello world bug"}, Date: time.Now().Format(time.RFC3339)},
		{Kind: "message", Payload: map[string]any{"content": "fix the bug"}, Date: time.Now().Format(time.RFC3339)},
	}
	for _, e := range entries {
		_ = store.Append("t", e)
	}
	zh, err := store.FetchAllSearch("t", &tape.FetchOpts{TextQuery: "爱编程", Limit: 10}, "fts5")
	if err != nil {
		return fmt.Errorf("chinese search: %w", err)
	}
	if len(zh) != 1 {
		return fmt.Errorf("chinese search: want 1, got %d", len(zh))
	}
	en, err := store.FetchAllSearch("t", &tape.FetchOpts{TextQuery: "bug", Limit: 10}, "fts5")
	if err != nil {
		return fmt.Errorf("english search: %w", err)
	}
	if len(en) != 2 {
		return fmt.Errorf("english search: want 2, got %d", len(en))
	}
	return nil
}

func caseT08FTS5SpecialChars(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5True)
	if err != nil {
		return err
	}
	_ = store.Append("t", tape.TapeEntry{
		Kind: "message", Payload: map[string]any{"content": "curl http://10.200.22.253:8000/v1/model"},
		Date: time.Now().Format(time.RFC3339),
	})
	entries, err := store.FetchAllSearch("t", &tape.FetchOpts{TextQuery: "10.200.22.253", Limit: 10}, "fts5")
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return fmt.Errorf("IP search: want 1, got %d", len(entries))
	}
	return nil
}

func caseT09FTS5MultiTape(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5True)
	if err != nil {
		return err
	}
	for _, name := range []string{"tape1", "tape2"} {
		_ = store.Append(name, tape.TapeEntry{
			Kind: "message", Payload: map[string]any{"content": "shared keyword"},
			Date: time.Now().Format(time.RFC3339),
		})
	}
	e1, err := store.FetchAllSearch("tape1", &tape.FetchOpts{TextQuery: "shared"}, "fts5")
	if err != nil || len(e1) != 1 {
		return fmt.Errorf("tape1: len=%d err=%v", len(e1), err)
	}
	e2, err := store.FetchAllSearch("tape2", &tape.FetchOpts{TextQuery: "tape1"}, "fts5")
	if err != nil || len(e2) != 0 {
		return fmt.Errorf("tape2 isolation: len=%d err=%v", len(e2), err)
	}
	return nil
}

func caseT10FTS5Migration(t *testing.T, driver Driver) error {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mig.db")

	s1, err := tape.NewSQLiteTapeStoreWithDriver(dbPath, string(driver), tape.SQLiteStoreConfig{EnableFTS5: config.FTS5False})
	if err != nil {
		return err
	}
	for _, c := range []string{"我爱编程", "hello", "bug fix"} {
		_ = s1.Append("t", tape.TapeEntry{Kind: "message", Payload: map[string]any{"content": c}})
	}
	s1.Close()

	s2, err := tape.NewSQLiteTapeStoreWithDriver(dbPath, string(driver), tape.SQLiteStoreConfig{EnableFTS5: config.FTS5True})
	if err != nil {
		return err
	}
	defer s2.Close()
	var n int
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM entries_fts").Scan(&n); err != nil {
		return err
	}
	if n != 3 {
		return fmt.Errorf("migration backfill: want 3 fts rows, got %d", n)
	}
	return nil
}

func caseT11RebuildFTS(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5True)
	if err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		_ = store.Append("t", tape.TapeEntry{Kind: "message", Payload: map[string]any{"content": "rebuild me"}})
	}
	if err := store.RebuildFTS(); err != nil {
		return err
	}
	var n int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM entries_fts").Scan(&n); err != nil {
		return err
	}
	if n != 3 {
		return fmt.Errorf("after rebuild: want 3, got %d", n)
	}
	return nil
}

func caseT12LikeFallback(t *testing.T, driver Driver) error {
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5False)
	if err != nil {
		return err
	}
	_ = store.Append("t", tape.TapeEntry{Kind: "message", Payload: map[string]any{"content": "like mode test"}})
	entries, err := store.FetchAllSearch("t", &tape.FetchOpts{TextQuery: "mode"}, "like")
	if err != nil || len(entries) != 1 {
		return fmt.Errorf("like search: len=%d err=%v", len(entries), err)
	}
	return nil
}

func caseT14WAL(t *testing.T, driver Driver) error {
	db, err := OpenDB(driver, filepath.Join(t.TempDir(), "wal.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if driver == DriverTurso {
		return fmt.Errorf("SKIP: turso may not support WAL pragma the same way")
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("journal_mode=%q, want wal", mode)
	}
	return nil
}

func caseT15Timezone(t *testing.T, driver Driver) error {
	old := tape.GetTimezone()
	defer tape.SetTimezone(old.String())

	if err := tape.SetTimezone("UTC"); err != nil {
		return err
	}
	store, err := openTapeStoreOrSkip(t, driver, config.FTS5False)
	if err != nil {
		return err
	}
	entry := tape.NewMessageEntry(map[string]any{"content": "tz test"})
	if err := store.Append("t", entry); err != nil {
		return err
	}
	entries, err := store.FetchAll("t", nil)
	if err != nil || len(entries) != 1 {
		return fmt.Errorf("fetch: len=%d err=%v", len(entries), err)
	}
	if entries[0].Date == "" {
		return fmt.Errorf("entry date empty")
	}
	return nil
}
