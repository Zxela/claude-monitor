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

func TestSaveSession_ParentID(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	parent := &session.Session{
		ID:         "parent-1",
		StartedAt:  now.Add(-10 * time.Minute),
		LastActive: now,
	}
	child := &session.Session{
		ID:         "child-1",
		ParentID:   "parent-1",
		StartedAt:  now.Add(-5 * time.Minute),
		LastActive: now,
	}

	if err := db.SaveSession(parent); err != nil {
		t.Fatalf("SaveSession parent failed: %v", err)
	}
	if err := db.SaveSession(child); err != nil {
		t.Fatalf("SaveSession child failed: %v", err)
	}

	rows, err := db.ListHistory(10, 0)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	var childRow *HistoryRow
	for i := range rows {
		if rows[i].ID == "child-1" {
			childRow = &rows[i]
			break
		}
	}
	if childRow == nil {
		t.Fatal("child row not found")
	}
	if childRow.ParentID != "parent-1" {
		t.Errorf("ParentID: got %q, want 'parent-1'", childRow.ParentID)
	}

	var parentRow *HistoryRow
	for i := range rows {
		if rows[i].ID == "parent-1" {
			parentRow = &rows[i]
			break
		}
	}
	if parentRow == nil {
		t.Fatal("parent row not found")
	}
	if parentRow.ParentID != "" {
		t.Errorf("Parent's ParentID should be empty, got %q", parentRow.ParentID)
	}
}

func TestAggregateStats_All(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()

	// Two top-level sessions
	s1 := &session.Session{
		ID:              "top-1",
		TotalCost:       1.00,
		InputTokens:     100,
		OutputTokens:    50,
		CacheReadTokens: 20,
		CacheCreationTokens: 10,
		StartedAt:       now.Add(-10 * time.Minute),
		LastActive:      now,
		Model:           "claude-sonnet-4-6",
	}
	s2 := &session.Session{
		ID:              "top-2",
		TotalCost:       2.00,
		InputTokens:     200,
		OutputTokens:    100,
		CacheReadTokens: 40,
		CacheCreationTokens: 20,
		StartedAt:       now.Add(-5 * time.Minute),
		LastActive:      now,
		Model:           "claude-opus-4-6",
	}
	// One child session — should be excluded from aggregates
	child := &session.Session{
		ID:              "child-1",
		ParentID:        "top-1",
		TotalCost:       0.50,
		InputTokens:     50,
		OutputTokens:    25,
		CacheReadTokens: 10,
		StartedAt:       now.Add(-3 * time.Minute),
		LastActive:      now,
		Model:           "claude-sonnet-4-6",
	}

	for _, s := range []*session.Session{s1, s2, child} {
		if err := db.SaveSession(s); err != nil {
			t.Fatalf("SaveSession %s failed: %v", s.ID, err)
		}
	}

	agg, err := db.AggregateStats(time.Time{})
	if err != nil {
		t.Fatalf("AggregateStats failed: %v", err)
	}

	if agg.SessionCount != 2 {
		t.Errorf("SessionCount: got %d, want 2", agg.SessionCount)
	}
	if agg.TotalCost != 3.00 {
		t.Errorf("TotalCost: got %f, want 3.00", agg.TotalCost)
	}
	if agg.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", agg.InputTokens)
	}
	if agg.OutputTokens != 150 {
		t.Errorf("OutputTokens: got %d, want 150", agg.OutputTokens)
	}
	if agg.CacheReadTokens != 60 {
		t.Errorf("CacheReadTokens: got %d, want 60", agg.CacheReadTokens)
	}
	if agg.CacheCreationTokens != 30 {
		t.Errorf("CacheCreationTokens: got %d, want 30", agg.CacheCreationTokens)
	}
	if len(agg.CostByModel) != 2 {
		t.Errorf("CostByModel: got %d models, want 2", len(agg.CostByModel))
	}
	if agg.CostByModel["claude-sonnet-4-6"] != 1.00 {
		t.Errorf("CostByModel[sonnet]: got %f, want 1.00", agg.CostByModel["claude-sonnet-4-6"])
	}
	if agg.CostByModel["claude-opus-4-6"] != 2.00 {
		t.Errorf("CostByModel[opus]: got %f, want 2.00", agg.CostByModel["claude-opus-4-6"])
	}
}

func TestAggregateStats_Window(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()

	// Old session (2 hours ago)
	old := &session.Session{
		ID:          "old-1",
		TotalCost:   5.00,
		InputTokens: 500,
		StartedAt:   now.Add(-2 * time.Hour),
		LastActive:  now.Add(-1 * time.Hour),
		Model:       "claude-sonnet-4-6",
	}
	// Recent session (5 minutes ago)
	recent := &session.Session{
		ID:          "recent-1",
		TotalCost:   1.00,
		InputTokens: 100,
		StartedAt:   now.Add(-5 * time.Minute),
		LastActive:  now,
		Model:       "claude-sonnet-4-6",
	}

	for _, s := range []*session.Session{old, recent} {
		if err := db.SaveSession(s); err != nil {
			t.Fatalf("SaveSession %s failed: %v", s.ID, err)
		}
	}

	// Query with window that only includes recent session
	since := now.Add(-30 * time.Minute)
	agg, err := db.AggregateStats(since)
	if err != nil {
		t.Fatalf("AggregateStats failed: %v", err)
	}

	if agg.SessionCount != 1 {
		t.Errorf("SessionCount: got %d, want 1", agg.SessionCount)
	}
	if agg.TotalCost != 1.00 {
		t.Errorf("TotalCost: got %f, want 1.00", agg.TotalCost)
	}
	if agg.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", agg.InputTokens)
	}
}

func TestGetSessionSnapshots(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	sess := &session.Session{
		ID:                  "snap-1",
		TotalCost:           2.50,
		InputTokens:         300,
		OutputTokens:        150,
		CacheReadTokens:     60,
		CacheCreationTokens: 30,
		StartedAt:           now.Add(-10 * time.Minute),
		LastActive:          now,
	}
	if err := db.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	snaps, err := db.GetSessionSnapshots([]string{"snap-1", "nonexistent"})
	if err != nil {
		t.Fatalf("GetSessionSnapshots failed: %v", err)
	}

	snap, ok := snaps["snap-1"]
	if !ok {
		t.Fatal("expected snap-1 in result")
	}
	if snap.TotalCost != 2.50 {
		t.Errorf("TotalCost: got %f, want 2.50", snap.TotalCost)
	}
	if snap.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", snap.InputTokens)
	}
	if snap.OutputTokens != 150 {
		t.Errorf("OutputTokens: got %d, want 150", snap.OutputTokens)
	}
	if snap.CacheReadTokens != 60 {
		t.Errorf("CacheReadTokens: got %d, want 60", snap.CacheReadTokens)
	}
	if snap.CacheCreationTokens != 30 {
		t.Errorf("CacheCreationTokens: got %d, want 30", snap.CacheCreationTokens)
	}

	// Nonexistent ID should not be in the map
	if _, ok := snaps["nonexistent"]; ok {
		t.Error("nonexistent ID should not be in result")
	}

	// Empty IDs should return empty map
	empty, err := db.GetSessionSnapshots([]string{})
	if err != nil {
		t.Fatalf("GetSessionSnapshots empty failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected empty map, got %d entries", len(empty))
	}
}
