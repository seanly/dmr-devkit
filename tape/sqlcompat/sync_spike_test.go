//go:build turso_sync_eval

package sqlcompat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSyncSpike runs Turso Cloud sync smoke tests when TURSO_SYNC_URL and TURSO_AUTH_TOKEN are set.
func TestSyncSpike(t *testing.T) {
	url := os.Getenv("TURSO_SYNC_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")
	if url == "" || token == "" {
		t.Skip("TURSO_SYNC_URL and TURSO_AUTH_TOKEN required for sync spike")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := t.TempDir()
	localPath := filepath.Join(dir, "sync.db")
	rep := &Report{}

	run := func(id string, fn func() error) {
		if err := fn(); err != nil {
			rep.Add(id, DriverTurso, StatusFail, err.Error(), false)
			t.Logf("[%s] %v", id, err)
			return
		}
		rep.Add(id, DriverTurso, StatusPass, "", false)
	}

	run("S01", func() error {
		db, err := newTursoSyncDB(ctx, localPath, url, token, true)
		if err != nil {
			return err
		}
		return db.Close()
	})

	run("S02", func() error {
		db, err := newTursoSyncDB(ctx, localPath, url, token, false)
		if err != nil {
			return err
		}
		defer db.Close()
		conn, err := db.Connect(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sync_notes(id TEXT PRIMARY KEY, body TEXT)`); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO sync_notes VALUES ('n1', 'hello')`); err != nil {
			return err
		}
		if err := db.Push(ctx); err != nil {
			return err
		}
		_, err = db.Pull(ctx)
		return err
	})

	run("S03", func() error {
		db, err := newTursoSyncDB(ctx, localPath, url, token, false)
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Push(ctx)
	})

	t.Logf("\n=== Sync spike ===\n%s", rep.Summary())
}
