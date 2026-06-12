package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/session"
)

// TestReadsNotBlockedByWriteTransaction verifies that read queries are served
// from the read pool and complete while the single write connection is held
// by an open transaction (as ingest flushes and retention compaction do).
func TestReadsNotBlockedByWriteTransaction(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.SaveSession(&session.Session{
		ID:         "read-pool-session",
		LastActive: time.Now(),
		StartedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// Occupy the single write connection with an open transaction that holds
	// the SQLite write lock.
	tx, err := db.db.Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('read-pool-test', '1')`); err != nil {
		t.Fatalf("write inside transaction failed: %v", err)
	}

	type listResult struct {
		rows []SessionRow
		err  error
	}
	done := make(chan listResult, 1)
	go func() {
		rows, err := db.ListSessions(10, 0)
		done <- listResult{rows: rows, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ListSessions failed: %v", res.err)
		}
		if len(res.rows) != 1 {
			t.Errorf("ListSessions returned %d rows, want 1", len(res.rows))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read query blocked behind held write transaction")
	}
}

// TestOpenPathWithSpecialChars verifies the file: DSN escapes characters that
// SQLite's URI parser would otherwise treat as delimiters or percent-escapes.
func TestOpenPathWithSpecialChars(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "odd %dir #1?")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	path := filepath.Join(dir, "test.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", path, err)
	}
	defer db.Close()

	if err := db.SetSetting("k", "v"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}
	if got, err := db.GetSetting("k"); err != nil || got != "v" {
		t.Fatalf("GetSetting = %q, %v; want \"v\", nil", got, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file not created at literal path: %v", err)
	}
}

// TestReadPoolIsReadOnly verifies writes cannot sneak through the read pool.
func TestReadPoolIsReadOnly(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	_, err := db.rdb.Exec(`INSERT INTO settings (key, value) VALUES ('nope', '1')`)
	if err == nil {
		t.Fatal("write through read pool succeeded, want query_only error")
	}
	if !strings.Contains(err.Error(), "readonly") && !strings.Contains(err.Error(), "query_only") {
		t.Fatalf("unexpected error from read-only pool write: %v", err)
	}
}
