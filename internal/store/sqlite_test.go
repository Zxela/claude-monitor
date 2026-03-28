package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/repo"
	"github.com/zxela-claude/claude-monitor/internal/session"
)

func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpen_CreatesDatabase(t *testing.T) {
	t.Parallel()
	openTestDB(t) // just verifying it doesn't panic/error
}

func TestUpsertRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	r := &repo.Repo{ID: "github.com/test/repo", Name: "repo", URL: "git@github.com:test/repo.git"}
	if err := db.UpsertRepo(r); err != nil {
		t.Fatalf("UpsertRepo failed: %v", err)
	}
	// Update should not error
	r.Name = "updated-repo"
	if err := db.UpsertRepo(r); err != nil {
		t.Fatalf("UpsertRepo update failed: %v", err)
	}
}

func TestCwdRepos(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Insert repo first (FK constraint)
	db.UpsertRepo(&repo.Repo{ID: "test-repo", Name: "test"})

	if err := db.UpsertCwdRepo("/home/user/project", "test-repo"); err != nil {
		t.Fatalf("UpsertCwdRepo failed: %v", err)
	}

	entries, err := db.LoadCwdRepos()
	if err != nil {
		t.Fatalf("LoadCwdRepos failed: %v", err)
	}
	if entries["/home/user/project"] != "test-repo" {
		t.Errorf("expected test-repo, got %q", entries["/home/user/project"])
	}

	if err := db.ClearCwdRepos(); err != nil {
		t.Fatalf("ClearCwdRepos failed: %v", err)
	}
	entries, _ = db.LoadCwdRepos()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(entries))
	}
}

func TestSaveSession_InsertAndUpdate(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	sess := &session.Session{
		ID:              "test-1",
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
		t.Fatalf("SaveSession failed: %v", err)
	}

	rows, err := db.ListSessions(10, 0)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].TotalCost != 1.23 {
		t.Errorf("TotalCost: got %f, want 1.23", rows[0].TotalCost)
	}

	// Update
	sess.TotalCost = 2.50
	if err := db.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession update failed: %v", err)
	}
	rows, _ = db.ListSessions(10, 0)
	if rows[0].TotalCost != 2.50 {
		t.Errorf("TotalCost after update: got %f, want 2.50", rows[0].TotalCost)
	}
}

func TestAggregateStats_IncludesChildren(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	parent := &session.Session{
		ID: "parent-1", TotalCost: 2.00, InputTokens: 200,
		StartedAt: now.Add(-10 * time.Minute), LastActive: now,
		Model: "claude-opus-4-6",
	}
	child := &session.Session{
		ID: "child-1", ParentID: "parent-1", TotalCost: 0.50, InputTokens: 50,
		StartedAt: now.Add(-5 * time.Minute), LastActive: now,
		Model: "claude-sonnet-4-6",
	}

	db.SaveSession(parent)
	db.SaveSession(child)

	agg, err := db.AggregateStats(time.Time{})
	if err != nil {
		t.Fatalf("AggregateStats failed: %v", err)
	}

	// Both sessions should be counted (no parent_id filter)
	if agg.SessionCount != 2 {
		t.Errorf("SessionCount: got %d, want 2", agg.SessionCount)
	}
	if agg.TotalCost != 2.50 {
		t.Errorf("TotalCost: got %f, want 2.50", agg.TotalCost)
	}
	if agg.InputTokens != 250 {
		t.Errorf("InputTokens: got %d, want 250", agg.InputTokens)
	}
}

func TestPersistBatch(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Create a session first
	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{
		Events: []EventInsert{
			{
				SessionID: "s1",
				Event: &parser.Event{
					Type:        "assistant",
					Role:        "assistant",
					ContentText: "Hello, I can help with that.",
					CostUSD:     0.01,
					InputTokens: 100,
					OutputTokens: 50,
					Timestamp:   time.Now(),
					Model:       "claude-sonnet-4-6",
					UUID:        "uuid-001",
				},
				FullContent: "Hello, I can help with that. Let me look at the code.",
			},
			{
				SessionID: "s1",
				Event: &parser.Event{
					Type:        "assistant",
					Role:        "assistant",
					ContentText: "[tool: Read]",
					ToolName:    "Read",
					ToolDetail:  "/home/user/parser.go",
					Timestamp:   time.Now().Add(time.Second),
					UUID:        "uuid-002",
				},
			},
		},
	}

	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}

	events, err := db.ListEvents("s1", 100, 0)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ContentPreview != "Hello, I can help with that." {
		t.Errorf("event 0 content: got %q", events[0].ContentPreview)
	}
	if events[0].FullContent != "Hello, I can help with that. Let me look at the code." {
		t.Errorf("event 0 fullContent: got %q", events[0].FullContent)
	}
	if events[1].ToolName != "Read" {
		t.Errorf("event 1 toolName: got %q", events[1].ToolName)
	}
}

func TestPersistBatch_Dedup(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	ts := time.Now()
	// First insert
	batch1 := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			MessageID:   "msg-1",
			Type:        "assistant",
			ContentText: "partial...",
			CostUSD:     0.005,
			InputTokens: 50,
			Timestamp:   ts,
		},
	}}}
	if err := db.PersistBatch(batch1); err != nil {
		t.Fatalf("PersistBatch 1 failed: %v", err)
	}

	// Second insert with same message_id (streaming update)
	batch2 := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			MessageID:   "msg-1",
			Type:        "assistant",
			ContentText: "full response here",
			CostUSD:     0.01,
			InputTokens: 100,
			Timestamp:   ts,
		},
	}}}
	if err := db.PersistBatch(batch2); err != nil {
		t.Fatalf("PersistBatch 2 failed: %v", err)
	}

	events, _ := db.ListEvents("s1", 100, 0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event after dedup, got %d", len(events))
	}
	if events[0].ContentPreview != "full response here" {
		t.Errorf("expected updated content, got %q", events[0].ContentPreview)
	}
	if events[0].CostUSD != 0.01 {
		t.Errorf("expected updated cost 0.01, got %f", events[0].CostUSD)
	}
}

func TestSearchFTS(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "reading parser.go",
				ToolName: "Read", ToolDetail: "/home/user/parser.go",
				Timestamp: time.Now(),
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "running tests",
				ToolName: "Bash", ToolDetail: "go test ./...",
				Timestamp: time.Now().Add(time.Second),
			},
		},
	}}
	db.PersistBatch(batch)

	results, err := db.SearchFTS("parser", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].ToolName != "Read" {
		t.Errorf("expected Read tool, got %q", results[0].ToolName)
	}
}

func TestSettings(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Defaults from migration
	val, err := db.GetSetting("retention_hot_days")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "30" {
		t.Errorf("expected 30, got %q", val)
	}

	// Update
	if err := db.SetSetting("retention_hot_days", "14"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}
	val, _ = db.GetSetting("retention_hot_days")
	if val != "14" {
		t.Errorf("expected 14 after update, got %q", val)
	}

	// All settings
	all, err := db.AllSettings()
	if err != nil {
		t.Fatalf("AllSettings failed: %v", err)
	}
	if len(all) < 2 {
		t.Errorf("expected at least 2 settings, got %d", len(all))
	}
}

func TestListRecentEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	ts := time.Now()
	batch := &EventBatch{}
	for i := 0; i < 10; i++ {
		batch.Events = append(batch.Events, EventInsert{
			SessionID: "s1",
			Event: &parser.Event{
				Type:        "assistant",
				ContentText: fmt.Sprintf("msg %d", i),
				Timestamp:   ts.Add(time.Duration(i) * time.Second),
				UUID:        fmt.Sprintf("uuid-%d", i),
			},
		})
	}
	db.PersistBatch(batch)

	events, err := db.ListRecentEvents("s1", 3)
	if err != nil {
		t.Fatalf("ListRecentEvents failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// Should be in chronological order (oldest first of the last 3)
	if events[0].ContentPreview != "msg 7" {
		t.Errorf("expected msg 7 first, got %q", events[0].ContentPreview)
	}
	if events[2].ContentPreview != "msg 9" {
		t.Errorf("expected msg 9 last, got %q", events[2].ContentPreview)
	}
}
