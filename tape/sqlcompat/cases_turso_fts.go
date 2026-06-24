//go:build turso_eval

package sqlcompat

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// RunTursoFTSCases evaluates Turso native FTS (Tantivy) against DMR tape/memory search scenarios.
// See https://docs.turso.tech/sql-reference/functions/fts
func RunTursoFTSCases(t *testing.T, rep *Report) {
	t.Helper()
	dbPath := t.TempDir() + "/turso_fts.db"

	avail, probeErr := TursoNativeFTSAvailable(dbPath)
	if !avail {
		msg := "unknown"
		if probeErr != nil {
			msg = probeErr.Error()
		}
		rep.Add("F00", DriverTurso, StatusSkip, "native FTS unavailable in tursogo build: "+msg, true)
		t.Skip("Turso native FTS not available: ", msg)
		return
	}
	rep.Add("F00", DriverTurso, StatusPass, "CREATE INDEX ... USING fts", false)

	db, err := OpenTursoDB(dbPath)
	if err != nil {
		rep.Add("F00", DriverTurso, StatusFail, err.Error(), true)
		t.Fatalf("open turso: %v", err)
	}
	defer db.Close()

	run := func(id string, blocker bool, fn func(*sql.DB) error) {
		if err := fn(db); err != nil {
			if strings.HasPrefix(err.Error(), "SKIP:") {
				rep.Add(id, DriverTurso, StatusSkip, strings.TrimPrefix(err.Error(), "SKIP: "), false)
				return
			}
			rep.Add(id, DriverTurso, StatusFail, err.Error(), blocker)
			t.Logf("[%s/turso-fts] %v", id, err)
			return
		}
		rep.Add(id, DriverTurso, StatusPass, "", false)
	}

	run("F01", true, caseF01TapeSchemaIndex)
	run("F02", true, caseF02ChineseNgram)
	run("F03", true, caseF03EnglishSearch)
	run("F04", true, caseF04TapeScopedSearch)
	run("F05", false, caseF05SpecialChars)
	run("F06", true, caseF06InsertAutoIndex)
	run("F07", false, caseF07DeleteRemovesHit)
	run("F08", false, caseF08OptimizeIndex)
}

func initTapeTursoFTS(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tape TEXT NOT NULL,
		kind TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '{}',
		meta TEXT NOT NULL DEFAULT '{}'
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`DROP INDEX IF EXISTS idx_entries_fts`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX idx_entries_fts ON entries USING fts (
		payload WITH tokenizer=ngram,
		meta WITH tokenizer=ngram
	)`)
	return err
}

func caseF01TapeSchemaIndex(db *sql.DB) error {
	return initTapeTursoFTS(db)
}

func caseF02ChineseNgram(db *sql.DB) error {
	if err := initTapeTursoFTS(db); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO entries (tape, kind, payload, meta) VALUES
		('t', 'message', '{"content":"我爱编程 Go语言"}', '{}'),
		('t', 'message', '{"content":"编程是乐趣"}', '{}')`); err != nil {
		return err
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE tape='t' AND fts_match(payload, meta, '编程')`).Scan(&n)
	if err != nil {
		return err
	}
	if n < 1 {
		return fmt.Errorf("chinese ngram: want >=1 row, got %d", n)
	}
	return nil
}

func caseF03EnglishSearch(db *sql.DB) error {
	if err := initTapeTursoFTS(db); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO entries (tape, kind, payload, meta) VALUES
		('t', 'message', '{"content":"hello world bug"}', '{}'),
		('t', 'message', '{"content":"fix the bug"}', '{}')`); err != nil {
		return err
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE tape='t' AND fts_match(payload, meta, 'bug')`).Scan(&n)
	if err != nil {
		return err
	}
	if n != 2 {
		return fmt.Errorf("english search: want 2 rows, got %d", n)
	}
	return nil
}

func caseF04TapeScopedSearch(db *sql.DB) error {
	if err := initTapeTursoFTS(db); err != nil {
		return err
	}
	for _, tape := range []string{"tape1", "tape2"} {
		if _, err := db.Exec(`INSERT INTO entries (tape, kind, payload, meta) VALUES (?, 'message', '{"content":"shared keyword"}', '{}')`, tape); err != nil {
			return err
		}
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE tape='tape1' AND fts_match(payload, meta, 'shared')`).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("tape1 scoped: want 1, got %d", n)
	}
	err = db.QueryRow(`SELECT COUNT(*) FROM entries WHERE tape='tape2' AND fts_match(payload, meta, 'tape1')`).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("tape2 isolation: want 0, got %d", n)
	}
	return nil
}

func caseF05SpecialChars(db *sql.DB) error {
	if err := initTapeTursoFTS(db); err != nil {
		return err
	}
	body := `curl http://10.200.22.52:8000/v1/model`
	if _, err := db.Exec(`INSERT INTO entries (tape, kind, payload, meta) VALUES ('t', 'message', ?, '{}')`, `{"content":"`+body+`"}`); err != nil {
		return err
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE tape='t' AND fts_match(payload, meta, '10.200.22.52')`).Scan(&n)
	if err != nil {
		return fmt.Errorf("SKIP: IP-style query may need quoting: %v", err)
	}
	if n < 1 {
		return fmt.Errorf("IP search: want >=1, got %d", n)
	}
	return nil
}

func caseF06InsertAutoIndex(db *sql.DB) error {
	if err := initTapeTursoFTS(db); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO entries (tape, kind, payload, meta) VALUES ('t', 'message', '{"content":"auto index keyword"}', '{}')`); err != nil {
		return err
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE fts_match(payload, meta, 'auto')`).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("auto index after insert: want 1, got %d", n)
	}
	return nil
}

func caseF07DeleteRemovesHit(db *sql.DB) error {
	if err := initTapeTursoFTS(db); err != nil {
		return err
	}
	res, err := db.Exec(`INSERT INTO entries (tape, kind, payload, meta) VALUES ('t', 'message', '{"content":"deleteme uniquexyz"}', '{}')`)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`DELETE FROM entries WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := db.Exec(`OPTIMIZE INDEX idx_entries_fts`); err != nil {
		return fmt.Errorf("SKIP: OPTIMIZE after delete: %v", err)
	}
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM entries WHERE fts_match(payload, meta, 'uniquexyz')`).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("post-delete search: want 0, got %d", n)
	}
	return nil
}

func caseF08OptimizeIndex(db *sql.DB) error {
	if err := initTapeTursoFTS(db); err != nil {
		return err
	}
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(`INSERT INTO entries (tape, kind, payload, meta) VALUES ('t', 'message', '{"content":"bulk optimize test"}', '{}')`); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`OPTIMIZE INDEX idx_entries_fts`); err != nil {
		return err
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE fts_match(payload, meta, 'optimize')`).Scan(&n)
	if err != nil {
		return err
	}
	if n < 1 {
		return fmt.Errorf("after optimize: want >=1, got %d", n)
	}
	return nil
}
