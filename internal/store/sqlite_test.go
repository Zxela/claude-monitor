package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/session"
)

func tempDBPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db")
}

func TestOpen_CreatesDatabase(t *testing.T) {
	t.Parallel()
	path := tempDBPath(t)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestSaveSession_InsertsAndUpdates(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	sess := &session.Session{
		ID:              "test-session-1",
		ProjectName:     "test-project",
		SessionName:     "my session",
		TotalCost:       1.23,
		InputTokens:     1000,
		OutputTokens:    500,
		CacheReadTokens: 200,
		MessageCount:    10,
		ErrorCount:      1,
		StartedAt:       now.Add(-10 * time.Minute),
		LastActive:      now,
		Model:           "claude-sonnet-4-6",
		CWD:             "/tmp/test",
		GitBranch:       "main",
		TaskDescription: "Fix the bug",
	}

	if err := db.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession insert failed: %v", err)
	}

	rows, err := db.ListHistory(10, 0)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.ID != "test-session-1" {
		t.Errorf("ID: got %q, want test-session-1", r.ID)
	}
	if r.ProjectName != "test-project" {
		t.Errorf("ProjectName: got %q, want test-project", r.ProjectName)
	}
	if r.TotalCost != 1.23 {
		t.Errorf("TotalCost: got %f, want 1.23", r.TotalCost)
	}
	if r.TaskDescription != "Fix the bug" {
		t.Errorf("TaskDescription: got %q, want 'Fix the bug'", r.TaskDescription)
	}

	// Update the session
	sess.TotalCost = 2.50
	if err := db.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession update failed: %v", err)
	}

	rows, err = db.ListHistory(10, 0)
	if err != nil {
		t.Fatalf("ListHistory after update failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(rows))
	}
	if rows[0].TotalCost != 2.50 {
		t.Errorf("TotalCost after update: got %f, want 2.50", rows[0].TotalCost)
	}
}

func TestListHistory_LimitAndOffset(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	for i := 0; i < 5; i++ {
		sess := &session.Session{
			ID:         "sess-" + string(rune('a'+i)),
			StartedAt:  now.Add(-time.Duration(5-i) * time.Minute),
			LastActive: now.Add(-time.Duration(5-i) * time.Minute).Add(time.Minute),
		}
		if err := db.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession %d failed: %v", i, err)
		}
	}

	// Get all
	rows, err := db.ListHistory(10, 0)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}

	// Limit 2
	rows, err = db.ListHistory(2, 0)
	if err != nil {
		t.Fatalf("ListHistory limit failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows with limit, got %d", len(rows))
	}

	// Offset 3
	rows, err = db.ListHistory(10, 3)
	if err != nil {
		t.Fatalf("ListHistory offset failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows with offset 3, got %d", len(rows))
	}
}

func TestListHistory_OrderByEndedAtDesc(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	// Insert sessions with different end times
	for i, id := range []string{"old", "mid", "new"} {
		sess := &session.Session{
			ID:         id,
			StartedAt:  now.Add(-time.Duration(3-i) * time.Hour),
			LastActive: now.Add(-time.Duration(3-i) * time.Hour).Add(30 * time.Minute),
		}
		if err := db.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession %s failed: %v", id, err)
		}
	}

	rows, err := db.ListHistory(10, 0)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	// Most recent ended_at should be first
	if rows[0].ID != "new" {
		t.Errorf("expected newest first, got %q", rows[0].ID)
	}
	if rows[2].ID != "old" {
		t.Errorf("expected oldest last, got %q", rows[2].ID)
	}
}
