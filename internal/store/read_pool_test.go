package store

import (
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

	done := make(chan error, 1)
	go func() {
		rows, err := db.ListSessions(10, 0)
		if err == nil && len(rows) != 1 {
			t.Errorf("ListSessions returned %d rows, want 1", len(rows))
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ListSessions failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read query blocked behind held write transaction")
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
