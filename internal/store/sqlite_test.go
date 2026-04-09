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

func TestPersistBatch_EventMetadataRoundTrip(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type:        "assistant",
			ContentText: "working on feature",
			Timestamp:   time.Now(),
			UUID:        "uuid-meta-001",
			ToolUseIDs:  []string{"tu-1", "tu-2", "tu-3"},
			CWD:         "/home/user/project",
			GitBranch:   "feature/new-thing",
			IsSidechain: true,
			AgentName:   "code-reviewer",
			TeamName:    "engineering",
		},
	}}}

	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}

	events, err := db.ListEvents("s1", 100, 0)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ev := events[0]
	if ev.ToolUseIDs != `["tu-1","tu-2","tu-3"]` {
		t.Errorf("ToolUseIDs: got %q, want %q", ev.ToolUseIDs, `["tu-1","tu-2","tu-3"]`)
	}
	if ev.CWD != "/home/user/project" {
		t.Errorf("CWD: got %q, want %q", ev.CWD, "/home/user/project")
	}
	if ev.GitBranch != "feature/new-thing" {
		t.Errorf("GitBranch: got %q, want %q", ev.GitBranch, "feature/new-thing")
	}
	if !ev.IsSidechain {
		t.Error("IsSidechain: got false, want true")
	}
	if ev.AgentName != "code-reviewer" {
		t.Errorf("AgentName: got %q, want %q", ev.AgentName, "code-reviewer")
	}
	if ev.TeamName != "engineering" {
		t.Errorf("TeamName: got %q, want %q", ev.TeamName, "engineering")
	}
}

// --- Trend test helpers ---

func insertTestSession(t *testing.T, db *DB, id string, startedAt time.Time, cost float64, input, output, cacheRead, cacheCreate int64) {
	t.Helper()
	s := &session.Session{
		ID:                  id,
		TotalCost:           cost,
		InputTokens:         input,
		OutputTokens:        output,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreate,
		StartedAt:           startedAt,
		LastActive:          startedAt.Add(5 * time.Minute),
		Model:               "claude-sonnet-4-6",
	}
	if err := db.SaveSession(s); err != nil {
		t.Fatalf("insertTestSession(%s): %v", id, err)
	}
}

func insertTestSessionWithRepo(t *testing.T, db *DB, id, repoID string, startedAt time.Time, cost float64, input, output, cacheRead, cacheCreate int64) {
	t.Helper()
	db.UpsertRepo(&repo.Repo{ID: repoID, Name: repoID})
	s := &session.Session{
		ID:                  id,
		RepoID:              repoID,
		TotalCost:           cost,
		InputTokens:         input,
		OutputTokens:        output,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreate,
		StartedAt:           startedAt,
		LastActive:          startedAt.Add(5 * time.Minute),
		Model:               "claude-sonnet-4-6",
	}
	if err := db.SaveSession(s); err != nil {
		t.Fatalf("insertTestSessionWithRepo(%s): %v", id, err)
	}
}

func insertTestSessionWithModel(t *testing.T, db *DB, id, model string, startedAt time.Time, cost float64, input, output, cacheRead, cacheCreate int64) {
	t.Helper()
	s := &session.Session{
		ID:                  id,
		TotalCost:           cost,
		InputTokens:         input,
		OutputTokens:        output,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreate,
		StartedAt:           startedAt,
		LastActive:          startedAt.Add(5 * time.Minute),
		Model:               model,
	}
	if err := db.SaveSession(s); err != nil {
		t.Fatalf("insertTestSessionWithModel(%s): %v", id, err)
	}
}

func TestTrendBuckets_DailyGrouping(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// Two sessions on day 1 (2 days ago), one session on day 2 (today)
	insertTestSession(t, db, "s1", now.Add(-48*time.Hour), 1.00, 100, 50, 30, 10)
	insertTestSession(t, db, "s2", now.Add(-47*time.Hour), 2.00, 200, 100, 60, 20)
	insertTestSession(t, db, "s3", now.Add(-1*time.Hour), 3.00, 300, 150, 90, 30)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if result.Window != "7d" {
		t.Errorf("Window: got %q, want 7d", result.Window)
	}

	// Should have 2 daily buckets
	if len(result.Buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(result.Buckets))
	}

	// First bucket (yesterday): 2 sessions, cost = 3.00
	b0 := result.Buckets[0]
	if b0.SessionCount != 2 {
		t.Errorf("bucket 0 sessions: got %d, want 2", b0.SessionCount)
	}
	if b0.Cost != 3.00 {
		t.Errorf("bucket 0 cost: got %f, want 3.00", b0.Cost)
	}

	// Second bucket (today): 1 session, cost = 3.00
	b1 := result.Buckets[1]
	if b1.SessionCount != 1 {
		t.Errorf("bucket 1 sessions: got %d, want 1", b1.SessionCount)
	}
	if b1.Cost != 3.00 {
		t.Errorf("bucket 1 cost: got %f, want 3.00", b1.Cost)
	}

	// Summary
	if result.Summary.TotalCost != 6.00 {
		t.Errorf("summary totalCost: got %f, want 6.00", result.Summary.TotalCost)
	}
	if result.Summary.SessionCount != 3 {
		t.Errorf("summary sessionCount: got %d, want 3", result.Summary.SessionCount)
	}
}

func TestTrendBuckets_EmptyWindow(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if result.Buckets == nil {
		t.Error("buckets should be empty slice, not nil")
	}
	if len(result.Buckets) != 0 {
		t.Errorf("expected 0 buckets, got %d", len(result.Buckets))
	}
	if result.Summary.SessionCount != 0 {
		t.Errorf("summary sessionCount: got %d, want 0", result.Summary.SessionCount)
	}
}

func TestTrendBuckets_RepoFilter(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	insertTestSessionWithRepo(t, db, "s1", "repo-a", now.Add(-2*time.Hour), 1.00, 100, 50, 30, 10)
	insertTestSessionWithRepo(t, db, "s2", "repo-b", now.Add(-1*time.Hour), 5.00, 500, 250, 150, 50)

	// Filter by repo-a
	result, err := db.TrendData("7d", "repo-a")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if result.Summary.SessionCount != 1 {
		t.Errorf("filtered sessionCount: got %d, want 1", result.Summary.SessionCount)
	}
	if result.Summary.TotalCost != 1.00 {
		t.Errorf("filtered totalCost: got %f, want 1.00", result.Summary.TotalCost)
	}
}

func TestTrendPercentiles(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// 20 sessions with costs 1..20, all on same day
	for i := 1; i <= 20; i++ {
		insertTestSession(t, db, fmt.Sprintf("s%d", i), now.Add(-time.Duration(i)*time.Minute), float64(i), 100, 50, 30, 10)
	}

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if len(result.Buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(result.Buckets))
	}

	b := result.Buckets[0]
	// Sorted costs: 1,2,3,...,20
	// Median (index 10): value = 11
	if b.MedianSessionCost != 11.0 {
		t.Errorf("median: got %f, want 11.0", b.MedianSessionCost)
	}
	// P95 (index 19): value = 20
	if b.P95SessionCost != 20.0 {
		t.Errorf("p95: got %f, want 20.0", b.P95SessionCost)
	}
}

func TestTrendByRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// repo-a: 2 sessions, total cost 3.00
	insertTestSessionWithRepo(t, db, "s1", "repo-a", now.Add(-2*time.Hour), 1.00, 100, 50, 30, 10)
	insertTestSessionWithRepo(t, db, "s2", "repo-a", now.Add(-1*time.Hour), 2.00, 200, 100, 60, 20)
	// repo-b: 1 session, total cost 5.00
	insertTestSessionWithRepo(t, db, "s3", "repo-b", now.Add(-30*time.Minute), 5.00, 500, 250, 150, 50)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if len(result.ByRepo) != 2 {
		t.Fatalf("expected 2 repo trends, got %d", len(result.ByRepo))
	}

	// Sorted by cost desc — repo-b first
	if result.ByRepo[0].RepoID != "repo-b" {
		t.Errorf("first repo: got %q, want repo-b", result.ByRepo[0].RepoID)
	}
	if result.ByRepo[0].Cost != 5.00 {
		t.Errorf("repo-b cost: got %f, want 5.00", result.ByRepo[0].Cost)
	}
	if result.ByRepo[0].Sessions != 1 {
		t.Errorf("repo-b sessions: got %d, want 1", result.ByRepo[0].Sessions)
	}

	if result.ByRepo[1].RepoID != "repo-a" {
		t.Errorf("second repo: got %q, want repo-a", result.ByRepo[1].RepoID)
	}
	if result.ByRepo[1].Cost != 3.00 {
		t.Errorf("repo-a cost: got %f, want 3.00", result.ByRepo[1].Cost)
	}
	if result.ByRepo[1].Sessions != 2 {
		t.Errorf("repo-a sessions: got %d, want 2", result.ByRepo[1].Sessions)
	}
}

func TestTrendByModel(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	insertTestSessionWithModel(t, db, "s1", "claude-opus-4-6", now.Add(-2*time.Hour), 4.00, 400, 200, 120, 40)
	insertTestSessionWithModel(t, db, "s2", "claude-sonnet-4-6", now.Add(-1*time.Hour), 1.00, 100, 50, 30, 10)
	insertTestSessionWithModel(t, db, "s3", "claude-opus-4-6", now.Add(-30*time.Minute), 3.00, 300, 150, 90, 30)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if len(result.ByModel) != 2 {
		t.Fatalf("expected 2 model trends, got %d", len(result.ByModel))
	}

	// Sorted by cost desc — opus first (7.00 vs 1.00)
	if result.ByModel[0].Model != "claude-opus-4-6" {
		t.Errorf("first model: got %q, want claude-opus-4-6", result.ByModel[0].Model)
	}
	if result.ByModel[0].Cost != 7.00 {
		t.Errorf("opus cost: got %f, want 7.00", result.ByModel[0].Cost)
	}
	if result.ByModel[0].Sessions != 2 {
		t.Errorf("opus sessions: got %d, want 2", result.ByModel[0].Sessions)
	}

	if result.ByModel[1].Model != "claude-sonnet-4-6" {
		t.Errorf("second model: got %q, want claude-sonnet-4-6", result.ByModel[1].Model)
	}
	if result.ByModel[1].Cost != 1.00 {
		t.Errorf("sonnet cost: got %f, want 1.00", result.ByModel[1].Cost)
	}
}

func TestPercentile(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := percentile(data, 0.5); got != 6 {
		t.Errorf("median of 1-10: got %v, want 6", got)
	}
	if got := percentile(data, 0.95); got != 10 {
		t.Errorf("p95 of 1-10: got %v, want 10", got)
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("percentile of nil: got %v, want 0", got)
	}
	if got := percentile([]float64{42}, 0.5); got != 42 {
		t.Errorf("percentile of single element: got %v, want 42", got)
	}
}
