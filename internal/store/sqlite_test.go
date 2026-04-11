package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
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

// =============================================================================
// TrendData — extended coverage
// =============================================================================

func TestTrendData_24hWindow(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now().UTC()
	// Sessions within the last 24h
	insertTestSession(t, db, "s1", now.Add(-20*time.Hour), 1.00, 100, 50, 30, 10)
	insertTestSession(t, db, "s2", now.Add(-10*time.Hour), 2.00, 200, 100, 60, 20)
	insertTestSession(t, db, "s3", now.Add(-1*time.Hour), 3.00, 300, 150, 90, 30)

	result, err := db.TrendData("24h", "")
	if err != nil {
		t.Fatalf("TrendData(24h) failed: %v", err)
	}

	if result.Window != "24h" {
		t.Errorf("Window: got %q, want 24h", result.Window)
	}

	// With 24h window, buckets are hourly — we should have at least 2 distinct hours
	if len(result.Buckets) < 2 {
		t.Errorf("expected at least 2 hourly buckets, got %d", len(result.Buckets))
	}
	if result.Summary.TotalCost != 6.00 {
		t.Errorf("summary totalCost: got %f, want 6.00", result.Summary.TotalCost)
	}
	if result.Summary.SessionCount != 3 {
		t.Errorf("summary sessionCount: got %d, want 3", result.Summary.SessionCount)
	}
}

func TestTrendData_30dWindow(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	insertTestSession(t, db, "s1", now.Add(-25*24*time.Hour), 1.00, 100, 50, 30, 10)
	insertTestSession(t, db, "s2", now.Add(-10*24*time.Hour), 2.00, 200, 100, 60, 20)
	insertTestSession(t, db, "s3", now.Add(-1*time.Hour), 3.00, 300, 150, 90, 30)

	result, err := db.TrendData("30d", "")
	if err != nil {
		t.Fatalf("TrendData(30d) failed: %v", err)
	}

	if result.Window != "30d" {
		t.Errorf("Window: got %q, want 30d", result.Window)
	}
	if len(result.Buckets) < 2 {
		t.Errorf("expected at least 2 daily buckets, got %d", len(result.Buckets))
	}
	if result.Summary.TotalCost != 6.00 {
		t.Errorf("summary totalCost: got %f, want 6.00", result.Summary.TotalCost)
	}
	if result.Summary.SessionCount != 3 {
		t.Errorf("summary sessionCount: got %d, want 3", result.Summary.SessionCount)
	}
}

func TestTrendData_InvalidWindow(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	_, err := db.TrendData("99d", "")
	if err == nil {
		t.Fatal("expected error for invalid window, got nil")
	}
}

func TestTrendData_ExcludesChildSessions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// Parent session
	db.SaveSession(&session.Session{
		ID: "parent-1", TotalCost: 5.00, InputTokens: 500,
		StartedAt: now.Add(-1 * time.Hour), LastActive: now,
		Model: "claude-sonnet-4-6",
	})
	// Child session — should be excluded by the parent_id filter in TrendData
	db.SaveSession(&session.Session{
		ID: "child-1", ParentID: "parent-1", TotalCost: 1.00, InputTokens: 100,
		StartedAt: now.Add(-30 * time.Minute), LastActive: now,
		Model: "claude-sonnet-4-6",
	})

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	// TrendData excludes child sessions (parent_id IS NULL OR parent_id = '')
	if result.Summary.SessionCount != 1 {
		t.Errorf("expected 1 session (parent only), got %d", result.Summary.SessionCount)
	}
	if result.Summary.TotalCost != 5.00 {
		t.Errorf("expected cost 5.00 (parent only), got %f", result.Summary.TotalCost)
	}
}

func TestTrendData_CacheHitPctCalculation(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// Session with known cache stats: input=100, cacheRead=300, cacheCreate=100
	// effInput = 100 + 300 + 100 = 500, cacheHitPct = 300/500 * 100 = 60%
	insertTestSession(t, db, "s1", now.Add(-1*time.Hour), 1.00, 100, 50, 300, 100)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if len(result.Buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(result.Buckets))
	}
	b := result.Buckets[0]
	if b.CacheHitPct != 60.0 {
		t.Errorf("CacheHitPct: got %f, want 60.0", b.CacheHitPct)
	}
	if result.Summary.CacheHitPct != 60.0 {
		t.Errorf("Summary CacheHitPct: got %f, want 60.0", result.Summary.CacheHitPct)
	}
}

func TestTrendData_OutputInputRatio(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// input=200, output=100, cacheRead=0, cacheCreate=0
	// effInput = 200, ratio = 100/200 = 0.5
	insertTestSession(t, db, "s1", now.Add(-1*time.Hour), 1.00, 200, 100, 0, 0)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if len(result.Buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(result.Buckets))
	}
	b := result.Buckets[0]
	if b.OutputInputRatio != 0.5 {
		t.Errorf("OutputInputRatio: got %f, want 0.5", b.OutputInputRatio)
	}
}

func TestTrendData_AvgSessionTokens(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now().UTC()
	// 2 sessions on same day: total tokens per session = input+output+cacheRead+cacheCreate
	// s1: 100+50+30+10 = 190
	// s2: 200+100+60+20 = 380
	// total = 570, avg = 285
	// Use small offsets to ensure both sessions fall within the same UTC day bucket
	insertTestSession(t, db, "s1", now.Add(-30*time.Minute), 1.00, 100, 50, 30, 10)
	insertTestSession(t, db, "s2", now.Add(-15*time.Minute), 2.00, 200, 100, 60, 20)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if len(result.Buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(result.Buckets))
	}
	// AvgSessionTokens = (effInput+output) / sessionCount
	// effInput = 300+90+30 = 420, output = 150, total = 420+150 = 570 ... wait
	// AvgSessionTokens is calculated as (effInput + outputTokens) / sessionCount
	// where effInput = inputTokens + cacheReadTokens + cacheCreationTokens
	// effInput = (100+30+10) + (200+60+20) = 140 + 280 = 420
	// output = 50 + 100 = 150
	// totalEffTokens = 420 + 150 = 570 ... but that's (effInput + output) not per session
	// Actually it's: b.AvgSessionTokens = float64(effInput+b.OutputTokens) / float64(b.SessionCount)
	// effInput per bucket = sum of (input + cacheRead + cacheCreate) = 300 + 90 + 30 = 420
	// outputTokens per bucket = 150
	// AvgSessionTokens = (420 + 150) / 2 = 285
	b := result.Buckets[0]
	if b.AvgSessionTokens != 285 {
		t.Errorf("AvgSessionTokens: got %f, want 285", b.AvgSessionTokens)
	}
}

func TestTrendData_WeeklySpanMultipleBuckets(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// Insert sessions across 5 different days
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("s%d", i)
		ts := now.Add(-time.Duration(i) * 24 * time.Hour)
		insertTestSession(t, db, id, ts, float64(i+1), int64((i+1)*100), int64((i+1)*50), 0, 0)
	}

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatalf("TrendData failed: %v", err)
	}

	if len(result.Buckets) != 5 {
		t.Fatalf("expected 5 daily buckets, got %d", len(result.Buckets))
	}

	// Verify buckets are sorted chronologically
	for i := 1; i < len(result.Buckets); i++ {
		if result.Buckets[i].Date < result.Buckets[i-1].Date {
			t.Errorf("buckets not sorted: bucket[%d]=%s < bucket[%d]=%s",
				i, result.Buckets[i].Date, i-1, result.Buckets[i-1].Date)
		}
	}

	// Total cost = 1+2+3+4+5 = 15
	if result.Summary.TotalCost != 15.00 {
		t.Errorf("summary totalCost: got %f, want 15.00", result.Summary.TotalCost)
	}
	if result.Summary.SessionCount != 5 {
		t.Errorf("summary sessionCount: got %d, want 5", result.Summary.SessionCount)
	}
}

// =============================================================================
// Compaction — hot-to-warm and warm-to-cold lifecycle
// =============================================================================

func TestCompactHotToWarm(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	// Insert events with timestamps old enough to be compacted (hotDays=0 means everything)
	oldTime := time.Now().Add(-48 * time.Hour)
	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "old event",
				Timestamp: oldTime, UUID: "uuid-old-1",
			},
			FullContent: "This is the full content of the old event that should be compressed.",
		},
	}}
	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}

	// Verify hot content exists
	events, _ := db.ListEvents("s1", 100, 0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].FullContent == "" {
		t.Fatal("expected full content before compaction")
	}

	// Compact with hotDays=0 — everything older than 0 days should be compacted
	count, err := db.CompactHotToWarm(0)
	if err != nil {
		t.Fatalf("CompactHotToWarm failed: %v", err)
	}
	if count != 1 {
		t.Errorf("compacted count: got %d, want 1", count)
	}

	// After compaction, ListEvents should transparently decompress the warm-tier
	// gzip BLOB and return the original full content.
	events, _ = db.ListEvents("s1", 100, 0)
	if events[0].FullContent != "This is the full content of the old event that should be compressed." {
		t.Errorf("expected decompressed full content after compaction, got %q", events[0].FullContent)
	}
}

func TestCompactHotToWarm_NoOldEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	// Insert a recent event
	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "recent event",
			Timestamp: time.Now(), UUID: "uuid-recent",
		},
		FullContent: "Recent full content.",
	}}}
	db.PersistBatch(batch)

	// Compact with hotDays=30 — recent event should NOT be compacted
	count, err := db.CompactHotToWarm(30)
	if err != nil {
		t.Fatalf("CompactHotToWarm failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 compacted, got %d", count)
	}

	// Content should still be accessible
	events, _ := db.ListEvents("s1", 100, 0)
	if events[0].FullContent != "Recent full content." {
		t.Errorf("full content should still be present, got %q", events[0].FullContent)
	}
}

func TestCompactWarmToCold(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	oldTime := time.Now().Add(-48 * time.Hour)
	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "old event to delete",
			Timestamp: oldTime, UUID: "uuid-delete-1",
		},
		FullContent: "Content to be fully deleted.",
	}}}
	db.PersistBatch(batch)

	// First compact hot to warm
	_, err := db.CompactHotToWarm(0)
	if err != nil {
		t.Fatalf("CompactHotToWarm failed: %v", err)
	}

	// Now compact warm to cold (warmDays=0 means delete everything)
	deleted, err := db.CompactWarmToCold(0)
	if err != nil {
		t.Fatalf("CompactWarmToCold failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted count: got %d, want 1", deleted)
	}

	// The event itself should still exist, but content row should be gone
	events, _ := db.ListEvents("s1", 100, 0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (metadata preserved), got %d", len(events))
	}
	if events[0].FullContent != "" {
		t.Errorf("expected no full content after cold compaction, got %q", events[0].FullContent)
	}
}

func TestCompactWarmToCold_NoOldEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "recent",
			Timestamp: time.Now(), UUID: "uuid-recent-cold",
		},
		FullContent: "Recent content.",
	}}}
	db.PersistBatch(batch)

	deleted, err := db.CompactWarmToCold(30)
	if err != nil {
		t.Fatalf("CompactWarmToCold failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
}

func TestCompactFullLifecycle(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	oldTime := time.Now().Add(-72 * time.Hour)
	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "lifecycle event",
			Timestamp: oldTime, UUID: "uuid-lifecycle",
		},
		FullContent: "Full lifecycle content: this text should survive hot->warm compression then be deleted in warm->cold.",
	}}}
	db.PersistBatch(batch)

	// Phase 1: hot — content is plaintext
	info1, _ := db.StorageInfo()
	if info1.HotContentBytes == 0 {
		t.Error("expected non-zero hot content bytes before compaction")
	}

	// Phase 2: compact to warm — content becomes gzip compressed
	compacted, err := db.CompactHotToWarm(0)
	if err != nil {
		t.Fatalf("CompactHotToWarm: %v", err)
	}
	if compacted != 1 {
		t.Errorf("compacted: got %d, want 1", compacted)
	}
	info2, _ := db.StorageInfo()
	if info2.HotContentBytes != 0 {
		t.Errorf("expected 0 hot content bytes after compaction, got %d", info2.HotContentBytes)
	}
	if info2.WarmContentBytes == 0 {
		t.Error("expected non-zero warm content bytes after compaction")
	}

	// Phase 3: compact to cold — content row deleted
	deleted, err := db.CompactWarmToCold(0)
	if err != nil {
		t.Fatalf("CompactWarmToCold: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted: got %d, want 1", deleted)
	}
	info3, _ := db.StorageInfo()
	if info3.WarmContentBytes != 0 {
		t.Errorf("expected 0 warm content bytes after cold compaction, got %d", info3.WarmContentBytes)
	}
	if info3.HotContentBytes != 0 {
		t.Errorf("expected 0 hot content bytes after cold compaction, got %d", info3.HotContentBytes)
	}

	// Event metadata should still exist
	events, _ := db.ListEvents("s1", 100, 0)
	if len(events) != 1 {
		t.Fatalf("expected event metadata preserved, got %d events", len(events))
	}
	if events[0].ContentPreview != "lifecycle event" {
		t.Errorf("content_preview should survive compaction, got %q", events[0].ContentPreview)
	}
}

// =============================================================================
// StorageInfo
// =============================================================================

func TestStorageInfo_EmptyDB(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	info, err := db.StorageInfo()
	if err != nil {
		t.Fatalf("StorageInfo failed: %v", err)
	}
	if info.EventCount != 0 {
		t.Errorf("EventCount: got %d, want 0", info.EventCount)
	}
	if info.HotContentBytes != 0 {
		t.Errorf("HotContentBytes: got %d, want 0", info.HotContentBytes)
	}
	if info.WarmContentBytes != 0 {
		t.Errorf("WarmContentBytes: got %d, want 0", info.WarmContentBytes)
	}
	if info.TotalSizeBytes <= 0 {
		t.Errorf("TotalSizeBytes should be positive (schema pages exist), got %d", info.TotalSizeBytes)
	}
}

func TestStorageInfo_WithData(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "event 1",
				Timestamp: time.Now(), UUID: "uuid-info-1",
			},
			FullContent: "Full content for event 1 to measure size.",
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "event 2",
				Timestamp: time.Now().Add(time.Second), UUID: "uuid-info-2",
			},
			FullContent: "Full content for event 2 to measure size.",
		},
	}}
	db.PersistBatch(batch)

	info, err := db.StorageInfo()
	if err != nil {
		t.Fatalf("StorageInfo failed: %v", err)
	}
	if info.EventCount != 2 {
		t.Errorf("EventCount: got %d, want 2", info.EventCount)
	}
	if info.HotContentBytes == 0 {
		t.Error("HotContentBytes should be > 0 with content stored")
	}
	if info.TotalSizeBytes <= 0 {
		t.Error("TotalSizeBytes should be > 0")
	}
}

// =============================================================================
// SearchFTS — extended coverage
// =============================================================================

func TestSearchFTS_MultipleResults(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "reading parser.go file",
				ToolName: "Read", ToolDetail: "/home/user/parser.go",
				Timestamp: time.Now(), UUID: "uuid-fts-1",
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "editing parser.go functions",
				ToolName: "Edit", ToolDetail: "/home/user/parser.go",
				Timestamp: time.Now().Add(time.Second), UUID: "uuid-fts-2",
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "running unit tests",
				ToolName: "Bash", ToolDetail: "go test ./...",
				Timestamp: time.Now().Add(2 * time.Second), UUID: "uuid-fts-3",
			},
		},
	}}
	db.PersistBatch(batch)

	results, err := db.SearchFTS("parser", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'parser', got %d", len(results))
	}
}

func TestSearchFTS_NoResults(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "hello world",
			Timestamp: time.Now(), UUID: "uuid-fts-noresult",
		},
	}}}
	db.PersistBatch(batch)

	results, err := db.SearchFTS("nonexistent", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchFTS_SpecialCharacters(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: `query with "quotes" and special chars`,
			Timestamp: time.Now(), UUID: "uuid-fts-special",
		},
	}}}
	db.PersistBatch(batch)

	// Should not error even with special FTS characters
	results, err := db.SearchFTS(`"quotes"`, 10)
	if err != nil {
		t.Fatalf("SearchFTS with special chars failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestSearchFTS_ByToolName(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "some text",
				ToolName: "Read", ToolDetail: "/path/to/file.go",
				Timestamp: time.Now(), UUID: "uuid-fts-tool-1",
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "other text",
				ToolName: "Bash", ToolDetail: "ls -la",
				Timestamp: time.Now().Add(time.Second), UUID: "uuid-fts-tool-2",
			},
		},
	}}
	db.PersistBatch(batch)

	// Search by tool detail
	results, err := db.SearchFTS("file.go", 10)
	if err != nil {
		t.Fatalf("SearchFTS by tool detail failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result matching tool detail, got %d", len(results))
	}
	if len(results) > 0 && results[0].ToolName != "Read" {
		t.Errorf("expected Read tool, got %q", results[0].ToolName)
	}
}

func TestSearchFTS_LimitRespected(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{}
	for i := 0; i < 10; i++ {
		batch.Events = append(batch.Events, EventInsert{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: fmt.Sprintf("keyword result %d", i),
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
				UUID:      fmt.Sprintf("uuid-fts-limit-%d", i),
			},
		})
	}
	db.PersistBatch(batch)

	results, err := db.SearchFTS("keyword", 3)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results with limit=3, got %d", len(results))
	}
}

func TestSearchFTS_DefaultLimit(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Passing limit=0 should default to 50
	results, err := db.SearchFTS("anything", 0)
	if err != nil {
		t.Fatalf("SearchFTS with limit=0 failed: %v", err)
	}
	// Just verify it does not error — no results is fine
	_ = results
}

// =============================================================================
// SearchFullContent
// =============================================================================

func TestSearchFullContent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "short preview",
				Timestamp: time.Now(), UUID: "uuid-fc-1",
			},
			FullContent: "This full content contains a SECRET_API_KEY=abc123 that we should detect.",
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "another preview",
				Timestamp: time.Now().Add(time.Second), UUID: "uuid-fc-2",
			},
			FullContent: "Normal full content without sensitive data.",
		},
	}}
	db.PersistBatch(batch)

	results, err := db.SearchFullContent("SECRET_API_KEY", 10)
	if err != nil {
		t.Fatalf("SearchFullContent failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for SECRET_API_KEY, got %d", len(results))
	}
	if results[0].UUID != "uuid-fc-1" {
		t.Errorf("expected uuid-fc-1, got %q", results[0].UUID)
	}
	// SearchFullContent joins event_content, so FullContent should be populated
	if results[0].FullContent == "" {
		t.Error("expected full content in search results")
	}
}

func TestSearchFullContent_NoResults(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "preview",
			Timestamp: time.Now(), UUID: "uuid-fc-none",
		},
		FullContent: "Normal content only.",
	}}}
	db.PersistBatch(batch)

	results, err := db.SearchFullContent("DOES_NOT_EXIST", 10)
	if err != nil {
		t.Fatalf("SearchFullContent failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchFullContent_DefaultLimit(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Passing limit=0 should default to 50
	results, err := db.SearchFullContent("anything", 0)
	if err != nil {
		t.Fatalf("SearchFullContent with limit=0 failed: %v", err)
	}
	_ = results
}

// =============================================================================
// Error paths — closed DB, invalid inputs
// =============================================================================

func TestClosedDB_ListSessions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.ListSessions(10, 0)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_SaveSession(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	err := db.SaveSession(&session.Session{
		ID: "x", StartedAt: time.Now(), LastActive: time.Now(),
	})
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_PersistBatch(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	err := db.PersistBatch(&EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "test",
			Timestamp: time.Now(), UUID: "uuid-closed",
		},
	}}})
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_AggregateStats(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.AggregateStats(time.Time{})
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_TrendData(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.TrendData("7d", "")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_SearchFTS(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.SearchFTS("test", 10)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_SearchFullContent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.SearchFullContent("test", 10)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_StorageInfo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	// StorageInfo swallows some errors but should still return a result
	info, err := db.StorageInfo()
	// It may or may not error depending on which query fails first
	_ = err
	_ = info
}

func TestClosedDB_CompactHotToWarm(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.CompactHotToWarm(0)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_CompactWarmToCold(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.CompactWarmToCold(0)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_UpsertRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	err := db.UpsertRepo(&repo.Repo{ID: "test", Name: "test"})
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_GetSetting(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.GetSetting("retention_hot_days")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_SetSetting(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	err := db.SetSetting("retention_hot_days", "7")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_AllSettings(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.AllSettings()
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestPersistBatch_EmptyBatch(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	err := db.PersistBatch(&EventBatch{})
	if err != nil {
		t.Errorf("empty batch should not error, got: %v", err)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	t.Parallel()
	_, err := Open("/nonexistent/deeply/nested/path/that/does/not/exist/test.db")
	if err == nil {
		t.Error("expected error opening DB at invalid path, got nil")
	}
}

func TestListSessions_DefaultLimits(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Negative limit and offset should be handled gracefully
	rows, err := db.ListSessions(-1, -1)
	if err != nil {
		t.Fatalf("ListSessions with negative args failed: %v", err)
	}
	// Should return empty (no data) without error
	if len(rows) != 0 {
		t.Errorf("expected 0 rows in empty DB, got %d", len(rows))
	}
}

func TestListEvents_DefaultLimit(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// limit=0 should default to 100
	events, err := db.ListEvents("nonexistent", 0, 0)
	if err != nil {
		t.Fatalf("ListEvents with limit=0 failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for nonexistent session, got %d", len(events))
	}
}

func TestListRecentEvents_DefaultN(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// n=0 should default to 50
	events, err := db.ListRecentEvents("nonexistent", 0)
	if err != nil {
		t.Fatalf("ListRecentEvents with n=0 failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for nonexistent session, got %d", len(events))
	}
}

// =============================================================================
// Additional coverage for existing methods
// =============================================================================

func TestGetSession(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Non-existent session should return nil, nil
	row, err := db.GetSession("nonexistent")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if row != nil {
		t.Error("expected nil for nonexistent session")
	}

	// Insert and retrieve
	now := time.Now()
	db.SaveSession(&session.Session{
		ID: "sess-get-1", SessionName: "my session", TotalCost: 3.50,
		StartedAt: now.Add(-5 * time.Minute), LastActive: now,
		Model: "claude-opus-4-6", CWD: "/tmp", GitBranch: "main",
	})

	row, err = db.GetSession("sess-get-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if row == nil {
		t.Fatal("expected non-nil session row")
	}
	if row.SessionName != "my session" {
		t.Errorf("SessionName: got %q, want %q", row.SessionName, "my session")
	}
	if row.TotalCost != 3.50 {
		t.Errorf("TotalCost: got %f, want 3.50", row.TotalCost)
	}
}

func TestListSessionsByRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	db.UpsertRepo(&repo.Repo{ID: "repo-x", Name: "repo-x"})
	db.UpsertRepo(&repo.Repo{ID: "repo-y", Name: "repo-y"})

	db.SaveSession(&session.Session{
		ID: "s1", RepoID: "repo-x", TotalCost: 1.00,
		StartedAt: now.Add(-10 * time.Minute), LastActive: now,
	})
	db.SaveSession(&session.Session{
		ID: "s2", RepoID: "repo-x", TotalCost: 2.00,
		StartedAt: now.Add(-5 * time.Minute), LastActive: now,
	})
	db.SaveSession(&session.Session{
		ID: "s3", RepoID: "repo-y", TotalCost: 5.00,
		StartedAt: now.Add(-2 * time.Minute), LastActive: now,
	})

	rows, err := db.ListSessionsByRepo("repo-x", 10, 0)
	if err != nil {
		t.Fatalf("ListSessionsByRepo failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 sessions for repo-x, got %d", len(rows))
	}
}

func TestAggregateStatsByRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	db.UpsertRepo(&repo.Repo{ID: "repo-agg", Name: "repo-agg"})

	db.SaveSession(&session.Session{
		ID: "s1", RepoID: "repo-agg", TotalCost: 1.00, InputTokens: 100, OutputTokens: 50,
		StartedAt: now.Add(-10 * time.Minute), LastActive: now,
		Model: "claude-sonnet-4-6",
	})
	db.SaveSession(&session.Session{
		ID: "s2", RepoID: "repo-agg", TotalCost: 2.00, InputTokens: 200, OutputTokens: 100,
		StartedAt: now.Add(-5 * time.Minute), LastActive: now,
		Model: "claude-opus-4-6",
	})

	agg, err := db.AggregateStatsByRepo("repo-agg")
	if err != nil {
		t.Fatalf("AggregateStatsByRepo failed: %v", err)
	}
	if agg.TotalCost != 3.00 {
		t.Errorf("TotalCost: got %f, want 3.00", agg.TotalCost)
	}
	if agg.SessionCount != 2 {
		t.Errorf("SessionCount: got %d, want 2", agg.SessionCount)
	}
	if agg.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", agg.InputTokens)
	}
	if len(agg.CostByModel) != 2 {
		t.Errorf("CostByModel count: got %d, want 2", len(agg.CostByModel))
	}
	if agg.CostByRepo["repo-agg"] != 3.00 {
		t.Errorf("CostByRepo[repo-agg]: got %f, want 3.00", agg.CostByRepo["repo-agg"])
	}
}

func TestAggregateStats_WithSinceFilter(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	// Session from 2 hours ago
	db.SaveSession(&session.Session{
		ID: "s-old", TotalCost: 1.00, InputTokens: 100,
		StartedAt: now.Add(-2 * time.Hour), LastActive: now.Add(-2 * time.Hour),
		Model: "claude-sonnet-4-6",
	})
	// Session from 30 minutes ago
	db.SaveSession(&session.Session{
		ID: "s-new", TotalCost: 3.00, InputTokens: 300,
		StartedAt: now.Add(-30 * time.Minute), LastActive: now,
		Model: "claude-sonnet-4-6",
	})

	// Filter to last hour
	agg, err := db.AggregateStats(now.Add(-1 * time.Hour))
	if err != nil {
		t.Fatalf("AggregateStats failed: %v", err)
	}
	if agg.SessionCount != 1 {
		t.Errorf("SessionCount: got %d, want 1", agg.SessionCount)
	}
	if agg.TotalCost != 3.00 {
		t.Errorf("TotalCost: got %f, want 3.00", agg.TotalCost)
	}
}

func TestListRepos(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	db.UpsertRepo(&repo.Repo{ID: "repo-list-a", Name: "Alpha", URL: "https://example.com/alpha"})
	db.UpsertRepo(&repo.Repo{ID: "repo-list-b", Name: "Beta"})

	db.SaveSession(&session.Session{
		ID: "s1", RepoID: "repo-list-a", TotalCost: 5.00,
		StartedAt: now, LastActive: now,
	})
	db.SaveSession(&session.Session{
		ID: "s2", RepoID: "repo-list-b", TotalCost: 2.00,
		StartedAt: now, LastActive: now,
	})

	repos, err := db.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos failed: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	// Should be sorted by cost desc
	if repos[0].ID != "repo-list-a" {
		t.Errorf("first repo: got %q, want repo-list-a", repos[0].ID)
	}
	if repos[0].TotalCost != 5.00 {
		t.Errorf("first repo cost: got %f, want 5.00", repos[0].TotalCost)
	}
	if repos[0].URL != "https://example.com/alpha" {
		t.Errorf("first repo URL: got %q, want https://example.com/alpha", repos[0].URL)
	}
}

func TestLoadMessageDedup(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				MessageID:   "msg-dedup-1",
				Type:        "assistant",
				ContentText: "first message",
				CostUSD:     0.01,
				InputTokens: 100,
				OutputTokens: 50,
				Timestamp:   time.Now(),
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				MessageID:   "msg-dedup-2",
				Type:        "assistant",
				ContentText: "second message",
				CostUSD:     0.02,
				InputTokens: 200,
				OutputTokens: 100,
				Timestamp:   time.Now().Add(time.Second),
			},
		},
	}}
	db.PersistBatch(batch)

	ids, costs, err := db.LoadMessageDedup("s1")
	if err != nil {
		t.Fatalf("LoadMessageDedup failed: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 message IDs, got %d", len(ids))
	}
	if !ids["msg-dedup-1"] {
		t.Error("expected msg-dedup-1 in ids")
	}
	if !ids["msg-dedup-2"] {
		t.Error("expected msg-dedup-2 in ids")
	}
	if costs["msg-dedup-1"].CostUSD != 0.01 {
		t.Errorf("msg-dedup-1 cost: got %f, want 0.01", costs["msg-dedup-1"].CostUSD)
	}
	if costs["msg-dedup-2"].InputTokens != 200 {
		t.Errorf("msg-dedup-2 input tokens: got %d, want 200", costs["msg-dedup-2"].InputTokens)
	}
}

func TestLoadMessageDedup_EmptySession(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	ids, costs, err := db.LoadMessageDedup("nonexistent")
	if err != nil {
		t.Fatalf("LoadMessageDedup failed: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 ids, got %d", len(ids))
	}
	if len(costs) != 0 {
		t.Errorf("expected 0 costs, got %d", len(costs))
	}
}

func TestListPinnedEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "normal event",
				Timestamp: time.Now(), UUID: "uuid-pinned-1",
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "error event", IsError: true,
				Timestamp: time.Now().Add(time.Second), UUID: "uuid-pinned-2",
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "[agent started]", IsAgent: true,
				Timestamp: time.Now().Add(2 * time.Second), UUID: "uuid-pinned-3",
			},
		},
	}}
	db.PersistBatch(batch)

	pinned, err := db.ListPinnedEvents("s1")
	if err != nil {
		t.Fatalf("ListPinnedEvents failed: %v", err)
	}
	// Should return error and agent events, not the normal one
	if len(pinned) != 2 {
		t.Fatalf("expected 2 pinned events, got %d", len(pinned))
	}
}

func TestSessionRowToSession(t *testing.T) {
	t.Parallel()

	row := SessionRow{
		ID:          "s1",
		RepoID:      "repo-1",
		ParentID:    "parent-1",
		SessionName: "test session",
		TotalCost:   5.00,
		InputTokens: 1000,
		StartedAt:   "2026-04-08T10:00:00Z",
		EndedAt:     "2026-04-08T11:00:00Z",
		Model:       "claude-opus-4-6",
		Version:     "1.0.0",
		Entrypoint:  "cli",
	}

	s := row.ToSession()
	if s.ID != "s1" {
		t.Errorf("ID: got %q, want s1", s.ID)
	}
	if s.RepoID != "repo-1" {
		t.Errorf("RepoID: got %q, want repo-1", s.RepoID)
	}
	if s.ParentID != "parent-1" {
		t.Errorf("ParentID: got %q, want parent-1", s.ParentID)
	}
	if s.TotalCost != 5.00 {
		t.Errorf("TotalCost: got %f, want 5.00", s.TotalCost)
	}
	if s.StartedAt.IsZero() {
		t.Error("StartedAt should be parsed")
	}
	if s.LastActive.IsZero() {
		t.Error("LastActive should be parsed from EndedAt")
	}
	if s.IsActive {
		t.Error("IsActive should default to false")
	}
	if s.Status != "idle" {
		t.Errorf("Status: got %q, want idle", s.Status)
	}
	if s.Version != "1.0.0" {
		t.Errorf("Version: got %q, want 1.0.0", s.Version)
	}
	if s.Entrypoint != "cli" {
		t.Errorf("Entrypoint: got %q, want cli", s.Entrypoint)
	}
}

func TestSessionRowToSession_EmptyTimestamps(t *testing.T) {
	t.Parallel()

	row := SessionRow{
		ID:        "s2",
		StartedAt: "",
		EndedAt:   "",
	}

	s := row.ToSession()
	if !s.StartedAt.IsZero() {
		t.Error("StartedAt should be zero for empty string")
	}
	if !s.LastActive.IsZero() {
		t.Error("LastActive should be zero for empty string")
	}
}

func TestSessionRowsToSessions(t *testing.T) {
	t.Parallel()

	rows := []SessionRow{
		{ID: "s1", TotalCost: 1.00},
		{ID: "s2", TotalCost: 2.00},
		{ID: "s3", TotalCost: 3.00},
	}

	sessions := SessionRowsToSessions(rows)
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "s1" {
		t.Errorf("sessions[0].ID: got %q, want s1", sessions[0].ID)
	}
	if sessions[2].TotalCost != 3.00 {
		t.Errorf("sessions[2].TotalCost: got %f, want 3.00", sessions[2].TotalCost)
	}
}

func TestPing(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestPing_ClosedDB(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	err := db.Ping()
	if err == nil {
		t.Error("expected error from Ping on closed DB, got nil")
	}
}

func TestClosedDB_LoadCwdRepos(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.LoadCwdRepos()
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_ClearCwdRepos(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	err := db.ClearCwdRepos()
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_GetSession(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.GetSession("test")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_ListSessionsByRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.ListSessionsByRepo("repo", 10, 0)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_AggregateStatsByRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.AggregateStatsByRepo("repo")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_ListRepos(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.ListRepos()
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_LoadMessageDedup(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, _, err := db.LoadMessageDedup("s1")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_ListEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.ListEvents("s1", 10, 0)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_ListPinnedEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.ListPinnedEvents("s1")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_ListRecentEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	_, err := db.ListRecentEvents("s1", 10)
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

func TestClosedDB_UpsertCwdRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	db.Close()

	err := db.UpsertCwdRepo("/tmp", "repo")
	if err == nil {
		t.Error("expected error from closed DB, got nil")
	}
}

// =============================================================================
// Additional coverage: GetSession field verification
// =============================================================================

func TestGetSession_AllFields(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	sess := &session.Session{
		ID:              "get-all-1",
		SessionName:     "full field test",
		TotalCost:       3.14,
		InputTokens:     500,
		OutputTokens:    250,
		CacheReadTokens: 100,
		MessageCount:    5,
		ErrorCount:      1,
		StartedAt:       now.Add(-10 * time.Minute),
		LastActive:      now,
		Model:           "claude-sonnet-4-6",
		CWD:             "/home/user/project",
		GitBranch:       "feature/test",
		TaskDescription: "Implement feature X",
	}
	if err := db.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	row, err := db.GetSession("get-all-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if row == nil {
		t.Fatal("expected session row, got nil")
	}
	if row.Model != "claude-sonnet-4-6" {
		t.Errorf("Model: got %q, want %q", row.Model, "claude-sonnet-4-6")
	}
	if row.CWD != "/home/user/project" {
		t.Errorf("CWD: got %q, want %q", row.CWD, "/home/user/project")
	}
	if row.Branch != "feature/test" {
		t.Errorf("Branch: got %q, want %q", row.Branch, "feature/test")
	}
	if row.TaskDescription != "Implement feature X" {
		t.Errorf("TaskDescription: got %q, want %q", row.TaskDescription, "Implement feature X")
	}
	if row.InputTokens != 500 {
		t.Errorf("InputTokens: got %d, want 500", row.InputTokens)
	}
	if row.ErrorCount != 1 {
		t.Errorf("ErrorCount: got %d, want 1", row.ErrorCount)
	}
}

// =============================================================================
// Additional coverage: SearchFullContent special characters
// =============================================================================

func TestSearchFullContent_SpecialCharacters(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.SaveSession(&session.Session{
		ID: "sfc-special", StartedAt: time.Now(), LastActive: time.Now(),
	})

	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "sfc-special",
		Event: &parser.Event{
			Type:        "assistant",
			ContentText: "special chars",
			Timestamp:   time.Now(),
			UUID:        "sfc-special-uuid",
		},
		FullContent: "line with 100% match and $pecial ch@racters",
	}}}
	db.PersistBatch(batch)

	results, err := db.SearchFullContent("$pecial ch@racters", 10)
	if err != nil {
		t.Fatalf("SearchFullContent: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for special characters, got %d", len(results))
	}
}

// =============================================================================
// Additional coverage: ListSessionsByRepo empty and pagination
// =============================================================================

func TestListSessionsByRepo_Empty(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	rows, err := db.ListSessionsByRepo("nonexistent-repo", 10, 0)
	if err != nil {
		t.Fatalf("ListSessionsByRepo: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 sessions for nonexistent repo, got %d", len(rows))
	}
}

func TestListSessionsByRepo_Pagination(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now()
	for i := 0; i < 5; i++ {
		insertTestSessionWithRepo(t, db, fmt.Sprintf("lsr-pg-%d", i), "repo-paged",
			now.Add(-time.Duration(i)*time.Hour), float64(i+1), 100, 50, 30, 10)
	}

	// First page
	page1, err := db.ListSessionsByRepo("repo-paged", 2, 0)
	if err != nil {
		t.Fatalf("ListSessionsByRepo page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1: expected 2, got %d", len(page1))
	}

	// Second page
	page2, err := db.ListSessionsByRepo("repo-paged", 2, 2)
	if err != nil {
		t.Fatalf("ListSessionsByRepo page 2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page 2: expected 2, got %d", len(page2))
	}

	// Pages should have different sessions
	if page1[0].ID == page2[0].ID {
		t.Error("page 1 and page 2 returned the same first session")
	}
}
