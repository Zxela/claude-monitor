# Data Store Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make SQLite the authoritative data source with server-side merge, add configurable time window to topbar, normalize field naming, and fix calculation bugs.

**Architecture:** New `/api/stats` endpoint queries SQLite aggregates and merges in-memory deltas for active sessions. Frontend topbar polls this endpoint instead of computing aggregates client-side. Field naming normalized to `totalCost` everywhere.

**Tech Stack:** Go (backend), TypeScript/vanilla DOM (frontend), SQLite (storage)

**Spec:** `docs/superpowers/specs/2026-03-27-data-store-reconciliation-design.md`

---

### Task 1: Migration 004 — add `cache_hit_pct` column

**Files:**
- Create: `internal/store/migrations/004_add_cache_hit_pct.go`
- Test: `internal/store/migrations/registry_test.go` (run existing)

- [ ] **Step 1: Write the migration file**

```go
// internal/store/migrations/004_add_cache_hit_pct.go
package migrations

import "database/sql"

func init() {
	Register(4, Migration{
		Name: "add_cache_hit_pct",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE session_history ADD COLUMN cache_hit_pct REAL DEFAULT 0`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	})
}
```

- [ ] **Step 2: Run migration tests to verify registration**

Run: `go test ./internal/store/migrations/ -v -run TestRegistry`
Expected: PASS (existing registry tests confirm version 4 is registered)

- [ ] **Step 3: Run full store tests to verify migration applies cleanly**

Run: `go test ./internal/store/ -v -count=1`
Expected: All PASS (Open() calls RunUp which applies the new migration)

- [ ] **Step 4: Commit**

```bash
git add internal/store/migrations/004_add_cache_hit_pct.go
git commit -m "feat: migration 004 adds cache_hit_pct column to session_history"
```

---

### Task 2: `SaveSession` writes `cache_hit_pct`; add `AggregateStats` and `GetSessionSnapshots`

**Files:**
- Modify: `internal/store/sqlite.go`
- Test: `internal/store/sqlite_test.go`

- [ ] **Step 1: Write failing tests for `AggregateStats`**

Add to `internal/store/sqlite_test.go`:

```go
func TestAggregateStats_All(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	sessions := []*session.Session{
		{
			ID: "s1", TotalCost: 5.0, InputTokens: 1000, OutputTokens: 500,
			CacheReadTokens: 300, CacheCreationTokens: 100, Model: "claude-opus-4-6",
			StartedAt: now.Add(-2 * time.Hour), LastActive: now.Add(-1 * time.Hour),
		},
		{
			ID: "s2", TotalCost: 3.0, InputTokens: 800, OutputTokens: 200,
			CacheReadTokens: 400, CacheCreationTokens: 50, Model: "claude-sonnet-4-6",
			StartedAt: now.Add(-30 * time.Minute), LastActive: now.Add(-10 * time.Minute),
		},
		{
			ID: "child1", ParentID: "s1", TotalCost: 1.0, InputTokens: 100, OutputTokens: 50,
			Model: "claude-sonnet-4-6",
			StartedAt: now.Add(-90 * time.Minute), LastActive: now.Add(-80 * time.Minute),
		},
	}
	for _, s := range sessions {
		if err := db.SaveSession(s); err != nil {
			t.Fatalf("SaveSession %s: %v", s.ID, err)
		}
	}

	// "all" window — should exclude subagents
	result, err := db.AggregateStats(time.Time{})
	if err != nil {
		t.Fatalf("AggregateStats: %v", err)
	}
	if result.TotalCost != 8.0 {
		t.Errorf("TotalCost: got %f, want 8.0", result.TotalCost)
	}
	if result.InputTokens != 1800 {
		t.Errorf("InputTokens: got %d, want 1800", result.InputTokens)
	}
	if result.SessionCount != 2 {
		t.Errorf("SessionCount: got %d, want 2", result.SessionCount)
	}
	if result.CostByModel["claude-opus-4-6"] != 5.0 {
		t.Errorf("CostByModel opus: got %f, want 5.0", result.CostByModel["claude-opus-4-6"])
	}
	if result.CostByModel["claude-sonnet-4-6"] != 3.0 {
		t.Errorf("CostByModel sonnet: got %f, want 3.0", result.CostByModel["claude-sonnet-4-6"])
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
	old := &session.Session{
		ID: "old", TotalCost: 10.0, InputTokens: 5000,
		Model: "claude-opus-4-6",
		StartedAt: now.Add(-48 * time.Hour), LastActive: now.Add(-47 * time.Hour),
	}
	recent := &session.Session{
		ID: "recent", TotalCost: 2.0, InputTokens: 1000,
		Model: "claude-sonnet-4-6",
		StartedAt: now.Add(-1 * time.Hour), LastActive: now.Add(-30 * time.Minute),
	}
	for _, s := range []*session.Session{old, recent} {
		if err := db.SaveSession(s); err != nil {
			t.Fatalf("SaveSession %s: %v", s.ID, err)
		}
	}

	// Window: last 24 hours — should only include "recent"
	since := now.Add(-24 * time.Hour)
	result, err := db.AggregateStats(since)
	if err != nil {
		t.Fatalf("AggregateStats: %v", err)
	}
	if result.TotalCost != 2.0 {
		t.Errorf("TotalCost: got %f, want 2.0", result.TotalCost)
	}
	if result.SessionCount != 1 {
		t.Errorf("SessionCount: got %d, want 1", result.SessionCount)
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
	s1 := &session.Session{
		ID: "snap1", TotalCost: 5.0, InputTokens: 1000, OutputTokens: 500,
		CacheReadTokens: 300, CacheCreationTokens: 100,
		StartedAt: now.Add(-1 * time.Hour), LastActive: now,
	}
	if err := db.SaveSession(s1); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	snaps, err := db.GetSessionSnapshots([]string{"snap1", "nonexistent"})
	if err != nil {
		t.Fatalf("GetSessionSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	snap := snaps["snap1"]
	if snap.TotalCost != 5.0 {
		t.Errorf("TotalCost: got %f, want 5.0", snap.TotalCost)
	}
	if snap.InputTokens != 1000 {
		t.Errorf("InputTokens: got %d, want 1000", snap.InputTokens)
	}
	if snap.CacheReadTokens != 300 {
		t.Errorf("CacheReadTokens: got %d, want 300", snap.CacheReadTokens)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -v -run "TestAggregateStats|TestGetSessionSnapshots" -count=1`
Expected: FAIL — `AggregateStats` and `GetSessionSnapshots` are undefined

- [ ] **Step 3: Update `SaveSession` to write `cache_hit_pct`**

In `internal/store/sqlite.go`, modify the `SaveSession` method. Add cache hit pct computation before the INSERT, and add the column to the INSERT/UPDATE:

```go
func (d *DB) SaveSession(s *session.Session) error {
	var endedAt string
	var duration float64
	if !s.LastActive.IsZero() {
		endedAt = s.LastActive.Format(time.RFC3339)
	}
	if !s.StartedAt.IsZero() && !s.LastActive.IsZero() {
		duration = s.LastActive.Sub(s.StartedAt).Seconds()
	}
	var startedAt string
	if !s.StartedAt.IsZero() {
		startedAt = s.StartedAt.Format(time.RFC3339)
	}

	var cacheHitPct float64
	totalInput := s.InputTokens + s.CacheReadTokens + s.CacheCreationTokens
	if totalInput > 0 {
		cacheHitPct = float64(s.CacheReadTokens) / float64(totalInput) * 100
	}

	_, err := d.db.Exec(`INSERT INTO session_history
		(id, project_name, session_name, total_cost, input_tokens, output_tokens,
		 cache_read_tokens, cache_creation_tokens, message_count, error_count, started_at, ended_at,
		 duration_seconds, model, cwd, git_branch, task_description, parent_id, cache_hit_pct)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		 project_name=excluded.project_name,
		 session_name=excluded.session_name,
		 total_cost=excluded.total_cost,
		 input_tokens=excluded.input_tokens,
		 output_tokens=excluded.output_tokens,
		 cache_read_tokens=excluded.cache_read_tokens,
		 cache_creation_tokens=excluded.cache_creation_tokens,
		 message_count=excluded.message_count,
		 error_count=excluded.error_count,
		 started_at=excluded.started_at,
		 ended_at=excluded.ended_at,
		 duration_seconds=excluded.duration_seconds,
		 model=excluded.model,
		 cwd=excluded.cwd,
		 git_branch=excluded.git_branch,
		 task_description=excluded.task_description,
		 parent_id=excluded.parent_id,
		 cache_hit_pct=excluded.cache_hit_pct`,
		s.ID, s.ProjectName, s.SessionName, s.TotalCost,
		s.InputTokens, s.OutputTokens, s.CacheReadTokens, s.CacheCreationTokens,
		s.MessageCount, s.ErrorCount,
		startedAt, endedAt, duration,
		s.Model, s.CWD, s.GitBranch, s.TaskDescription, s.ParentID,
		cacheHitPct,
	)
	return err
}
```

- [ ] **Step 4: Add `CacheHitPct` to `HistoryRow` and update `ListHistory`**

In `internal/store/sqlite.go`, add the field to `HistoryRow`:

```go
type HistoryRow struct {
	ID              string  `json:"id"`
	ProjectName     string  `json:"projectName"`
	SessionName     string  `json:"sessionName"`
	TotalCost       float64 `json:"totalCost"`
	InputTokens     int64   `json:"inputTokens"`
	OutputTokens    int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	CacheHitPct         float64 `json:"cacheHitPct"`
	MessageCount        int     `json:"messageCount"`
	ErrorCount      int     `json:"errorCount"`
	StartedAt       string  `json:"startedAt"`
	EndedAt         string  `json:"endedAt"`
	DurationSeconds float64 `json:"durationSeconds"`
	Model           string  `json:"model"`
	CWD             string  `json:"cwd"`
	GitBranch       string  `json:"gitBranch"`
	TaskDescription string  `json:"taskDescription"`
	ParentID        string  `json:"parentId"`
}
```

Update `ListHistory` SELECT to include `COALESCE(cache_hit_pct, 0)` and add to Scan:

```go
func (d *DB) ListHistory(limit, offset int) ([]HistoryRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.db.Query(`SELECT
		id, project_name, session_name, total_cost, input_tokens, output_tokens,
		cache_read_tokens, COALESCE(cache_creation_tokens, 0), COALESCE(cache_hit_pct, 0),
		message_count, error_count,
		started_at, ended_at, duration_seconds, model, cwd, git_branch,
		task_description, parent_id
		FROM session_history
		ORDER BY ended_at DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HistoryRow
	for rows.Next() {
		var r HistoryRow
		if err := rows.Scan(
			&r.ID, &r.ProjectName, &r.SessionName, &r.TotalCost,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens,
			&r.CacheCreationTokens, &r.CacheHitPct, &r.MessageCount, &r.ErrorCount,
			&r.StartedAt, &r.EndedAt,
			&r.DurationSeconds, &r.Model, &r.CWD, &r.GitBranch,
			&r.TaskDescription, &r.ParentID,
		); err != nil {
			return result, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
```

- [ ] **Step 5: Implement `AggregateStats`**

Add types and method to `internal/store/sqlite.go`:

```go
// AggregateResult holds summed stats from the history database.
type AggregateResult struct {
	TotalCost           float64            `json:"totalCost"`
	InputTokens         int64              `json:"inputTokens"`
	OutputTokens        int64              `json:"outputTokens"`
	CacheReadTokens     int64              `json:"cacheReadTokens"`
	CacheCreationTokens int64              `json:"cacheCreationTokens"`
	SessionCount        int                `json:"sessionCount"`
	CostByModel         map[string]float64 `json:"costByModel"`
}

// AggregateStats returns summed cost/token stats for top-level sessions.
// If since is non-zero, only sessions with started_at >= since are included.
func (d *DB) AggregateStats(since time.Time) (*AggregateResult, error) {
	result := &AggregateResult{CostByModel: make(map[string]float64)}

	var args []interface{}
	where := "WHERE parent_id = ''"
	if !since.IsZero() {
		where += " AND started_at >= ?"
		args = append(args, since.Format(time.RFC3339))
	}

	// Totals query
	totalQ := `SELECT COALESCE(SUM(total_cost),0), COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read_tokens),0),
		COALESCE(SUM(COALESCE(cache_creation_tokens,0)),0), COUNT(*)
		FROM session_history ` + where
	if err := d.db.QueryRow(totalQ, args...).Scan(
		&result.TotalCost, &result.InputTokens, &result.OutputTokens,
		&result.CacheReadTokens, &result.CacheCreationTokens, &result.SessionCount,
	); err != nil {
		return nil, err
	}

	// Per-model breakdown
	modelQ := `SELECT model, COALESCE(SUM(total_cost),0) FROM session_history ` + where + ` GROUP BY model`
	rows, err := d.db.Query(modelQ, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var model string
		var cost float64
		if err := rows.Scan(&model, &cost); err != nil {
			return result, err
		}
		if model == "" {
			model = "unknown"
		}
		result.CostByModel[model] = cost
	}

	return result, rows.Err()
}
```

- [ ] **Step 6: Implement `GetSessionSnapshots`**

Add to `internal/store/sqlite.go`:

```go
// SessionSnapshot holds the last-saved cost and token values for a session.
type SessionSnapshot struct {
	TotalCost           float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// GetSessionSnapshots returns the last-saved cost/token values for the given session IDs.
func (d *DB) GetSessionSnapshots(ids []string) (map[string]SessionSnapshot, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `SELECT id, total_cost, input_tokens, output_tokens,
		cache_read_tokens, COALESCE(cache_creation_tokens, 0)
		FROM session_history WHERE id IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]SessionSnapshot)
	for rows.Next() {
		var id string
		var snap SessionSnapshot
		if err := rows.Scan(&id, &snap.TotalCost, &snap.InputTokens,
			&snap.OutputTokens, &snap.CacheReadTokens, &snap.CacheCreationTokens); err != nil {
			return result, err
		}
		result[id] = snap
	}
	return result, rows.Err()
}
```

Note: `strings` must be added to the import block at the top of `sqlite.go`.

- [ ] **Step 7: Run all store tests**

Run: `go test ./internal/store/ -v -count=1`
Expected: All PASS including the three new tests

- [ ] **Step 8: Commit**

```bash
git add internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat: add AggregateStats, GetSessionSnapshots, and cache_hit_pct to SaveSession"
```

---

### Task 3: Normalize JSON field — `totalCostUSD` to `totalCost`

**Files:**
- Modify: `internal/session/session.go:39`
- Test: existing session tests

- [ ] **Step 1: Change the JSON tag**

In `internal/session/session.go`, change line 39:

```go
// Before:
TotalCost    float64   `json:"totalCostUSD"`
// After:
TotalCost    float64   `json:"totalCost"`
```

- [ ] **Step 2: Run tests to verify nothing breaks**

Run: `go test ./... -count=1`
Expected: All PASS (no test depends on JSON serialization of this field name)

- [ ] **Step 3: Commit**

```bash
git add internal/session/session.go
git commit -m "fix: rename JSON field totalCostUSD to totalCost for consistency with HistoryRow"
```

---

### Task 4: Add `/api/stats` endpoint with server-side merge

**Files:**
- Modify: `cmd/claude-monitor/main.go`
- Test: `cmd/claude-monitor/main_test.go` (if endpoint tests exist, otherwise manual verification)

- [ ] **Step 1: Add the `/api/stats` handler**

In `cmd/claude-monitor/main.go`, add this route after the existing `/api/sessions/grouped` handler (after line 599):

```go
// Aggregate stats with server-side merge of SQLite + live active sessions.
mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	now := time.Now()
	var since time.Time
	switch window {
	case "today":
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday = 7 for Monday-start
		}
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(weekday - 1))
	case "month":
		since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	default:
		// "all" or empty — no filter
	}

	agg, err := historyDB.AggregateStats(since)
	if err != nil {
		log.Printf("aggregate stats error: %v", err)
		writeJSONError(w, "failed to aggregate stats", http.StatusInternalServerError)
		return
	}

	// Merge live active sessions that may have unsaved deltas.
	allSessions := sessionStore.All()
	var activeIDs []string
	activeMap := make(map[string]*session.Session)
	var activeSessions int
	var costRate float64
	for _, sess := range allSessions {
		if sess.IsSubagent {
			continue
		}
		if sess.IsActive {
			activeSessions++
			costRate += sess.CostRate
		}
		// Check if session falls within the window
		inWindow := since.IsZero() || (!sess.StartedAt.IsZero() && !sess.StartedAt.Before(since))
		if inWindow && sess.IsActive {
			activeIDs = append(activeIDs, sess.ID)
			activeMap[sess.ID] = sess
		}
	}

	if len(activeIDs) > 0 {
		snapshots, err := historyDB.GetSessionSnapshots(activeIDs)
		if err != nil {
			log.Printf("get session snapshots error: %v", err)
			// Non-fatal: proceed with SQLite-only data
		} else {
			for _, id := range activeIDs {
				live := activeMap[id]
				saved, inDB := snapshots[id]
				if inDB {
					// Add delta between live and last-saved values
					agg.TotalCost += live.TotalCost - saved.TotalCost
					agg.InputTokens += live.InputTokens - saved.InputTokens
					agg.OutputTokens += live.OutputTokens - saved.OutputTokens
					agg.CacheReadTokens += live.CacheReadTokens - saved.CacheReadTokens
					agg.CacheCreationTokens += live.CacheCreationTokens - saved.CacheCreationTokens
				} else {
					// Not yet saved to SQLite — add full live values
					agg.TotalCost += live.TotalCost
					agg.InputTokens += live.InputTokens
					agg.OutputTokens += live.OutputTokens
					agg.CacheReadTokens += live.CacheReadTokens
					agg.CacheCreationTokens += live.CacheCreationTokens
					agg.SessionCount++
					model := live.Model
					if model == "" {
						model = "unknown"
					}
					agg.CostByModel[model] += live.TotalCost
				}
			}
		}
	}

	// Compute derived fields
	var cacheHitPct float64
	totalInput := agg.InputTokens + agg.CacheReadTokens + agg.CacheCreationTokens
	if totalInput > 0 {
		cacheHitPct = float64(agg.CacheReadTokens) / float64(totalInput) * 100
	}

	type statsResponse struct {
		TotalCost           float64            `json:"totalCost"`
		InputTokens         int64              `json:"inputTokens"`
		OutputTokens        int64              `json:"outputTokens"`
		CacheReadTokens     int64              `json:"cacheReadTokens"`
		CacheCreationTokens int64              `json:"cacheCreationTokens"`
		SessionCount        int                `json:"sessionCount"`
		ActiveSessions      int                `json:"activeSessions"`
		CacheHitPct         float64            `json:"cacheHitPct"`
		CostRate            float64            `json:"costRate"`
		CostByModel         map[string]float64 `json:"costByModel"`
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(statsResponse{
		TotalCost:           agg.TotalCost,
		InputTokens:         agg.InputTokens,
		OutputTokens:        agg.OutputTokens,
		CacheReadTokens:     agg.CacheReadTokens,
		CacheCreationTokens: agg.CacheCreationTokens,
		SessionCount:        agg.SessionCount,
		ActiveSessions:      activeSessions,
		CacheHitPct:         cacheHitPct,
		CostRate:            costRate,
		CostByModel:         agg.CostByModel,
	}); err != nil {
		log.Printf("json encode stats: %v", err)
	}
})
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 3: Manual smoke test**

Run: `go build -o /tmp/cm ./cmd/claude-monitor && /tmp/cm &`
Then: `curl -s http://localhost:7700/api/stats?window=all | jq .`
Expected: JSON with `totalCost`, `sessionCount`, `costByModel`, etc.
Cleanup: kill the background process

- [ ] **Step 4: Commit**

```bash
git add cmd/claude-monitor/main.go
git commit -m "feat: add /api/stats endpoint with server-side SQLite + live merge"
```

---

### Task 5: Fix `requireSession` skeleton pollution

**Files:**
- Modify: `cmd/claude-monitor/main.go:150-175`

- [ ] **Step 1: Replace the Upsert with a transient session**

In `cmd/claude-monitor/main.go`, replace the `requireSession` function:

```go
func requireSession(store *session.Store, w http.ResponseWriter, r *http.Request) (*session.Session, bool) {
	id := r.PathValue("id")
	sess, ok := store.Get(id)
	if !ok {
		// Try to find the JSONL file on disk for historical sessions.
		if sessionFinder != nil {
			if filePath := sessionFinder.FindSessionFile(id); filePath != "" {
				// Return transient session — do NOT persist to live store.
				return &session.Session{ID: id, FilePath: filePath}, true
			}
		}
		writeJSONError(w, "session not found", http.StatusNotFound)
		return nil, false
	}
	if sess.FilePath == "" {
		writeJSONError(w, "session file not available", http.StatusBadRequest)
		return nil, false
	}
	return sess, true
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/claude-monitor/main.go
git commit -m "fix: requireSession returns transient session instead of polluting live store"
```

---

### Task 6: Frontend — add `Stats` type, `fetchStats`, and state fields

**Files:**
- Modify: `web/src/types.ts`
- Modify: `web/src/api.ts`
- Modify: `web/src/state.ts`

- [ ] **Step 1: Add `Stats` interface to types.ts**

In `web/src/types.ts`, add after the `HistoryRow` interface:

```typescript
export interface Stats {
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  sessionCount: number;
  activeSessions: number;
  cacheHitPct: number;
  costRate: number;
  costByModel: Record<string, number>;
}
```

- [ ] **Step 2: Rename `totalCostUSD` to `totalCost` in Session interface**

In `web/src/types.ts`, change:

```typescript
// Before:
totalCostUSD: number;
// After:
totalCost: number;
```

- [ ] **Step 3: Add `cacheHitPct` to `HistoryRow` interface**

In `web/src/types.ts`, add to `HistoryRow`:

```typescript
// Add after cacheCreationTokens:
cacheHitPct: number;
```

- [ ] **Step 4: Add `fetchStats` to api.ts**

In `web/src/api.ts`, add:

```typescript
import type { GroupedSessions, SearchResult, HistoryRow, ParsedMessage, Stats } from './types';

export type StatsWindow = 'all' | 'today' | 'week' | 'month';

export async function fetchStats(window: StatsWindow = 'today'): Promise<Stats> {
  return request<Stats>(`${BASE}/api/stats?window=${window}`);
}
```

- [ ] **Step 5: Add stats fields to state.ts**

In `web/src/state.ts`, add imports and fields:

```typescript
import type { Session, GroupedSessions, SearchResult, Stats } from './types';
import type { StatsWindow } from './api';
```

Add to the `AppState` interface:

```typescript
stats: Stats | null;
statsWindow: StatsWindow;
```

Add to the `state` initialization:

```typescript
stats: null,
statsWindow: (localStorage.getItem('claude-monitor-stats-window') as StatsWindow) || 'today',
```

- [ ] **Step 6: Commit**

```bash
git add web/src/types.ts web/src/api.ts web/src/state.ts
git commit -m "feat: add Stats type, fetchStats API, and state fields for stats window"
```

---

### Task 7: Frontend — rewrite topbar to source from `/api/stats`

**Files:**
- Modify: `web/src/components/topbar.ts`

- [ ] **Step 1: Rewrite topbar.ts**

Replace the entire `topbar.ts` with the new version that sources from `state.stats`, removes WORKING, and adds the window selector. The key changes:

1. Remove `statWorking` element and references
2. Add window toggle buttons
3. Replace `updateStats()` client-side aggregation with rendering from `state.stats`
4. Add a 5s polling interval for `/api/stats`

In `web/src/components/topbar.ts`, make these changes:

Remove the `statWorking` variable declaration (line 13):

```typescript
// Remove this line:
let statWorking: HTMLElement | null = null;
```

Add imports at the top:

```typescript
import { fetchStats } from '../api';
import type { StatsWindow } from '../api';
```

Replace the HTML template — remove the WORKING stat, add window toggle. Change the `el.innerHTML` to:

```typescript
el.innerHTML = `
    <div class="topbar-brand">
      <span class="brand-diamond">◆</span>
      CLAUDE MONITOR
    </div>
    <div class="topbar-stat"><span>ACTIVE</span> <span class="val green" data-stat="active">0</span></div>
    <div class="topbar-stat" title="Total cost across all sessions"><span>TOTAL SPEND</span> <span class="budget-gear" role="button" tabindex="0" aria-label="Open budget and notification settings">⚙</span> <span class="val yellow" data-stat="cost">$0</span>
      <div class="window-toggle">
        <button class="win-btn" data-window="today">TODAY</button>
        <button class="win-btn" data-window="week">WEEK</button>
        <button class="win-btn" data-window="month">MONTH</button>
        <button class="win-btn" data-window="all">ALL</button>
      </div>
    </div>
    <div class="topbar-stat" title="Weighted cache read percentage across all sessions"><span>CACHE HIT</span> <span class="val" data-stat="cache" style="color:var(--purple)">—</span></div>
    <div class="topbar-stat" title="Aggregate cost velocity across all active sessions"><span>$/MIN</span> <span class="val yellow" data-stat="rate">—</span></div>
    <div class="search-box">
      <input type="text" placeholder="Search all sessions..." data-search aria-label="Search sessions" />
    </div>
    <nav class="view-toggle" aria-label="View selection">
      <button class="view-btn active" data-view="list" aria-pressed="true">LIST</button>
      <button class="view-btn" data-view="graph" aria-pressed="false">GRAPH</button>
      <button class="view-btn" data-view="history" aria-pressed="false">HISTORY</button>
    </nav>
  `;
```

After the element setup, remove the `statWorking` querySelector line and add window toggle listeners:

```typescript
// Remove:
statWorking = el.querySelector('[data-stat="working"]');

// Add window toggle handlers:
el.querySelectorAll<HTMLButtonElement>('.win-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    const win = btn.dataset.window as StatsWindow;
    localStorage.setItem('claude-monitor-stats-window', win);
    update({ statsWindow: win });
    refreshStats();
  });
});
// Set initial active state
updateWindowButtons();
```

Add a stats polling interval at the end of `render()`:

```typescript
// Start polling stats
refreshStats();
setInterval(refreshStats, 5000);
```

Add the `refreshStats` function:

```typescript
function refreshStats(): void {
  fetchStats(state.statsWindow).then(stats => {
    update({ stats });
  }).catch(() => { /* ignore — will retry in 5s */ });
}
```

Replace the `updateStats` function:

```typescript
function updateStats(): void {
  const stats = state.stats;
  if (!stats) return;

  setVal(statActive, String(stats.activeSessions));
  setVal(statCost, `$${stats.totalCost.toFixed(0)}`);
  setVal(statCache, stats.cacheHitPct > 0 ? `${stats.cacheHitPct.toFixed(0)}%` : '—');
  setVal(statRate, stats.costRate > 0 ? `$${stats.costRate.toFixed(3)}/m` : '—');
}
```

Add a function to highlight the active window button:

```typescript
function updateWindowButtons(): void {
  el?.querySelectorAll<HTMLButtonElement>('.win-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.window === state.statsWindow);
  });
}
```

Update `onStateChange` to react to `stats` and `statsWindow`:

```typescript
function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('stats')) {
    updateStats();
  }
  if (changed.has('statsWindow')) {
    updateWindowButtons();
  }
  if (changed.has('view')) {
    el?.querySelectorAll<HTMLButtonElement>('.view-btn').forEach(btn => {
      const isActive = btn.dataset.view === state.view;
      btn.classList.toggle('active', isActive);
      btn.setAttribute('aria-pressed', String(isActive));
    });
  }
}
```

Remove the `isSessionActive` import (no longer needed in topbar):

```typescript
// Remove from imports:
import { isSessionActive } from '../utils';
```

- [ ] **Step 2: Build and verify no TypeScript errors**

Run: `cd web && npx tsc --noEmit`
Expected: No errors (or only pre-existing ones)

- [ ] **Step 3: Commit**

```bash
git add web/src/components/topbar.ts
git commit -m "feat: topbar sources from /api/stats, adds time window selector, removes WORKING"
```

---

### Task 8: Frontend — rename `totalCostUSD` to `totalCost` in all components

**Files:**
- Modify: `web/src/components/session-card.ts`
- Modify: `web/src/components/graph-view.ts`
- Modify: `web/src/components/cost-breakdown.ts`
- Modify: `web/src/components/budget-popover.ts`

- [ ] **Step 1: session-card.ts — replace all `totalCostUSD` with `totalCost`**

Three occurrences at lines 65, 227:

```typescript
// Line 65 — change:
session.totalCostUSD  →  session.totalCost
// (both in getCostTier call and .toFixed(2))

// Line 227 — same change
session.totalCostUSD  →  session.totalCost
```

Also fix token total to include `cacheCreationTokens` (line 70):

```typescript
// Before:
${formatTokens(session.inputTokens + session.outputTokens + session.cacheReadTokens)} tok
// After:
${formatTokens(session.inputTokens + session.outputTokens + session.cacheReadTokens + session.cacheCreationTokens)} tok
```

- [ ] **Step 2: graph-view.ts — replace all `totalCostUSD` with `totalCost`**

Five occurrences at lines 137, 218, 342, 345, 389:

```typescript
sess.totalCostUSD  →  sess.totalCost        // lines 137, 218
n.session.totalCostUSD  →  n.session.totalCost  // lines 342, 345
node.session.totalCostUSD  →  node.session.totalCost  // line 389
```

- [ ] **Step 3: cost-breakdown.ts — replace all `totalCostUSD` with `totalCost`**

Four occurrences at lines 23, 30, 31, 127:

```typescript
s.totalCostUSD  →  s.totalCost   // lines 23, 30, 31, 127
```

- [ ] **Step 4: budget-popover.ts — replace `totalCostUSD` with `totalCost` and source from stats**

Rename on line 115:

```typescript
s.totalCostUSD  →  s.totalCost
```

Also refactor `checkBudget()` to use `state.stats` instead of iterating sessions, and update `onStateChange` to react to `stats` instead of `sessions`:

In the `onStateChange` function (line 46-49), change:

```typescript
// Before:
function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('sessions') || changed.has('budgetThreshold') || changed.has('budgetDismissed')) {
    checkBudget();
  }
}

// After:
function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('stats') || changed.has('budgetThreshold') || changed.has('budgetDismissed')) {
    checkBudget();
  }
}
```

In the `checkBudget` function (lines 111-134), replace the session iteration with stats:

```typescript
// Before:
const sessions = Array.from(state.sessions.values());
const total = sessions.reduce((sum, s) => sum + s.totalCostUSD, 0);

// After:
const total = state.stats?.totalCost ?? 0;
```

- [ ] **Step 5: Build and verify no TypeScript errors**

Run: `cd web && npx tsc --noEmit`
Expected: No errors referencing `totalCostUSD`

- [ ] **Step 6: Commit**

```bash
git add web/src/components/session-card.ts web/src/components/graph-view.ts web/src/components/cost-breakdown.ts web/src/components/budget-popover.ts
git commit -m "fix: rename totalCostUSD to totalCost across all frontend components"
```

---

### Task 9: Frontend — refactor cost-breakdown to use `state.stats`

**Files:**
- Modify: `web/src/components/cost-breakdown.ts`

- [ ] **Step 1: Refactor cost-breakdown to source from stats**

Replace the data sourcing in the `toggle` function. Instead of iterating `state.sessions.values()`, read from `state.stats`:

```typescript
export function toggle(anchor: HTMLElement): void {
  if (popover) { popover.remove(); popover = null; return; }

  const stats = state.stats;
  if (!stats) return;

  const byModel = new Map<string, number>();
  for (const [model, cost] of Object.entries(stats.costByModel)) {
    byModel.set(model, cost);
  }

  const totalInput = stats.inputTokens;
  const totalOutput = stats.outputTokens;
  const totalCache = stats.cacheReadTokens;
  const totalCost = stats.totalCost;

  // Top 5 costliest — still from session store (stats doesn't have per-session breakdown)
  const allSessions = Array.from(state.sessions.values());
  const top5 = [...allSessions].sort((a, b) => b.totalCost - a.totalCost).slice(0, 5);

  // ... rest of popover rendering stays the same, but remove the cbFilter
  // toggle buttons since the window is now controlled by the topbar
```

Remove the `cbFilter` variable and the ALL/TODAY filter buttons from the popover HTML. The window is now controlled by the topbar toggle.

Remove the filter button HTML block and its click handlers from the popover content.

- [ ] **Step 2: Build and verify**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/cost-breakdown.ts
git commit -m "refactor: cost-breakdown popover sources aggregates from state.stats"
```

---

### Task 10: Frontend — fix history-view to use `cacheHitPct` column and fix token total

**Files:**
- Modify: `web/src/components/history-view.ts`

- [ ] **Step 1: Replace cache% heuristic with pre-computed column**

In `web/src/components/history-view.ts`, change the `cacheHitPct` column definition (line 23):

```typescript
// Before:
{ key: 'cacheHitPct', label: 'Cache%', cls: 'col-cache', fmt: r => { if (!r.cacheCreationTokens && r.cacheReadTokens > 0) return '—'; const total = r.inputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0); return total > 0 ? `${Math.round(r.cacheReadTokens / total * 100)}%` : ''; } },

// After:
{ key: 'cacheHitPct', label: 'Cache%', cls: 'col-cache', fmt: r => r.cacheHitPct > 0 ? `${Math.round(r.cacheHitPct)}%` : '' },
```

- [ ] **Step 2: Fix token total to include cacheCreationTokens**

Change the tokens column (line 22):

```typescript
// Before:
{ key: 'tokens', label: 'Tokens', cls: 'col-tokens', fmt: r => formatTokens(r.inputTokens + r.outputTokens + r.cacheReadTokens) },

// After:
{ key: 'tokens', label: 'Tokens', cls: 'col-tokens', fmt: r => formatTokens(r.inputTokens + r.outputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0)) },
```

Also update the sort accessor for tokens (line 282):

```typescript
// Before:
case 'tokens': va = a.inputTokens + a.outputTokens + a.cacheReadTokens; vb = b.inputTokens + b.outputTokens + b.cacheReadTokens; break;

// After:
case 'tokens': va = a.inputTokens + a.outputTokens + a.cacheReadTokens + (a.cacheCreationTokens || 0); vb = b.inputTokens + b.outputTokens + b.cacheReadTokens + (b.cacheCreationTokens || 0); break;
```

And update the cacheHitPct sort accessor (line 292):

```typescript
// Before:
cacheHitPct: r => { const t = r.inputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0); return t > 0 ? r.cacheReadTokens / t * 100 : 0; },

// After:
cacheHitPct: r => r.cacheHitPct,
```

- [ ] **Step 3: Build and verify**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/history-view.ts
git commit -m "fix: history-view uses server-computed cacheHitPct, includes cacheCreationTokens in token total"
```

---

### Task 11: Frontend bootstrap — remove double fetch, verify init flow

**Files:**
- Modify: `web/src/main.ts`

- [ ] **Step 1: Review and verify init sequence**

The `main.ts` init flow calls `fetchGroupedSessions` then `connect()`. The WS `onopen` handler in `ws.ts` also calls `fetchGroupedSessions`. This is intentional for reconnect scenarios — the second fetch reconciles state missed during disconnect. No change needed here.

However, verify the topbar stats polling is initiated properly. The topbar `render()` function now calls `refreshStats()` and sets up a 5s interval. This happens at mount time in `main.ts` line 25 (`renderTopbar(topbarMount)`), before `connect()` is called. The first `fetchStats` call may fail if the server isn't ready, but the 5s retry handles this gracefully.

No code change needed. Mark as verified.

- [ ] **Step 2: Commit** (skip — no changes)

---

### Task 12: Add CSS for window toggle buttons

**Files:**
- Modify: `web/src/styles/topbar.css`

- [ ] **Step 1: Add window toggle styles**

```css
.window-toggle {
  display: inline-flex;
  gap: 2px;
  margin-left: 6px;
  vertical-align: middle;
}

.win-btn {
  font-family: var(--font-mono);
  font-size: 8px;
  padding: 1px 5px;
  background: none;
  border: 1px solid var(--border);
  color: var(--text-dim);
  cursor: pointer;
  border-radius: 2px;
  letter-spacing: 0.3px;
}

.win-btn.active {
  border-color: var(--cyan);
  color: var(--text);
}

.win-btn:hover {
  border-color: var(--text-dim);
}
```

- [ ] **Step 3: Build the frontend**

Run: `cd web && npm run build`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add web/src/styles/topbar.css
git commit -m "feat: add CSS for topbar stats window toggle buttons"
```

---

### Task 13: Full integration test

- [ ] **Step 1: Run all Go tests**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 2: Build the Go binary**

Run: `go build -o /tmp/cm ./cmd/claude-monitor`
Expected: Builds without errors

- [ ] **Step 3: Build the frontend**

Run: `cd web && npm run build`
Expected: Builds without errors

- [ ] **Step 4: Manual smoke test**

Start the server: `/tmp/cm &`

Verify endpoints:
- `curl -s http://localhost:7700/api/stats?window=all | jq .` — returns aggregate stats
- `curl -s http://localhost:7700/api/stats?window=today | jq .` — returns filtered stats
- `curl -s http://localhost:7700/api/history?limit=5 | jq '.[0] | keys'` — includes `cacheHitPct`
- `curl -s http://localhost:7700/api/sessions | jq '.[0].totalCost'` — field is `totalCost` (not `totalCostUSD`)

Cleanup: kill the background process

- [ ] **Step 5: Final commit (if any fixes needed)**

```bash
git add -A && git commit -m "fix: integration fixes from smoke test"
```
